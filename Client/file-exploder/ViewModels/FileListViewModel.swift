import Foundation
import SwiftUI

@MainActor
class FileListViewModel: ObservableObject {
    @Published var currentPath: String = "/"
    @Published var files: [RemoteFile] = []
    @Published var isLoading = false
    /// Published whole so the view can tell a failed listing - which leaves
    /// nothing to show - from a failed operation, which does not.
    @Published private(set) var errors = ErrorLog()

    var errorMessage: String? { errors.message }
    @Published var pathHistory: [String] = []
    @Published var pathHistoryIndex: Int = -1

    @AppStorage("showHiddenFiles") private var showHiddenFiles = false
    @AppStorage("refreshInterval") private var refreshInterval = 5.0
    private var refreshTask: Task<Void, Never>?
    private var navigationGeneration = 0
    private var settingsObserver: NSObjectProtocol?
    private var lastAppliedSettings: Settings?

    /// A queued job plus the display name it came from, so a failure that only
    /// surfaces while waiting can still say which file it was about.
    private struct QueuedOperation {
        let id: String
        let name: String
    }

    private struct Settings: Equatable {
        let showHiddenFiles: Bool
        let refreshInterval: Double
    }

    private(set) var sftp: SFTPService?
    private var ssh: SSHConnection?
    var hasConnection: Bool { sftp != nil }

    var canGoBack: Bool { pathHistoryIndex > 0 }
    var canGoForward: Bool { pathHistoryIndex < pathHistory.count - 1 }

    var remoteRoot: String {
        guard let root = ssh?.server.remoteRoot else { return "/" }
        return RemotePath.standardized(root)
    }

    var canGoToParent: Bool {
        let current = RemotePath.standardized(currentPath)
        let parent = RemotePath.parent(of: current)
        return parent != current && isPathAllowed(parent)
    }

    func connect(server: Server) async {
        disconnect()
        let sshConnection = SSHConnection(server: server)
        ssh = sshConnection
        sftp = SFTPService(ssh: sshConnection)

        lastAppliedSettings = currentSettings
        settingsObserver = NotificationCenter.default.addObserver(
            forName: UserDefaults.didChangeNotification,
            object: nil,
            queue: nil
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                await self?.applySettingsChangeIfNeeded()
            }
        }

        do {
            try await sshConnection.testConnection()
            guard ssh === sshConnection else { return }
            await navigateTo(path: server.remoteRoot)
            if let message = errorMessage {
                disconnect()
                errors.setListingError(message)
            }
        } catch {
            guard ssh === sshConnection else { return }
            let message = error.localizedDescription
            disconnect()
            errors.setListingError(message)
        }
    }

    deinit {
        // A window can be closed without disconnecting first, and
        // NotificationCenter retains the block regardless of its weak capture -
        // so every closed window would leave one behind, woken on every
        // defaults write for the life of the process.
        if let settingsObserver {
            NotificationCenter.default.removeObserver(settingsObserver)
        }
        refreshTask?.cancel()
        // Mirrors disconnect(): a window closed mid-operation must not leave
        // its SSH process running in the background just because nothing
        // called disconnect() first.
        ssh?.terminateAll()
    }

    func disconnect() {
        ssh?.terminateAll()
        refreshTask?.cancel()
        refreshTask = nil
        navigationGeneration += 1
        if let observer = settingsObserver {
            NotificationCenter.default.removeObserver(observer)
            settingsObserver = nil
        }

        ssh = nil
        sftp = nil
        lastAppliedSettings = nil
        files = []
        currentPath = "/"
        pathHistory = []
        pathHistoryIndex = -1
        errors.removeAll()
        isLoading = false
    }

    func navigateTo(path: String) async {
        dismissOperationErrors()
        guard isPathAllowed(path) else {
            reportOperationError("アクセスが許可されていません")
            return
        }
        if await loadPath(path, updateHistory: true) {
            startAutoRefresh()
        }
    }

    func goBack() async {
        dismissOperationErrors()
        guard canGoBack else { return }
        let newIndex = pathHistoryIndex - 1
        if await loadPath(pathHistory[newIndex], updateHistory: false) {
            pathHistoryIndex = newIndex
            startAutoRefresh()
        }
    }

    func goForward() async {
        dismissOperationErrors()
        guard canGoForward else { return }
        let newIndex = pathHistoryIndex + 1
        if await loadPath(pathHistory[newIndex], updateHistory: false) {
            pathHistoryIndex = newIndex
            startAutoRefresh()
        }
    }

    func goToParent() async {
        let current = RemotePath.standardized(currentPath)
        let parent = RemotePath.parent(of: current)
        guard parent != current, isPathAllowed(parent) else { return }
        await navigateTo(path: parent)
    }

    func refresh() async {
        if await loadPath(currentPath, updateHistory: false) {
            startAutoRefresh()
        }
    }

    /// The refresh the user asks for. Unlike the background and follow-up
    /// refreshes it drops the accumulated operation errors, so retrying visibly
    /// clears them when it works.
    func reload() async {
        dismissOperationErrors()
        await refresh()
    }

    func openFile(_ file: RemoteFile) async {
        if file.isDirectory {
            await navigateTo(path: file.path)
        }
    }

    func createFolder(name: String) async {
        guard let sftp else {
            reportOperationError("サーバーに接続されていません")
            return
        }
        guard let newPath = childPath(named: name) else {
            reportOperationError("フォルダ名に /、.、.. は使用できません")
            return
        }
        var finalError: String?
        do {
            let id = try await sftp.addToQueue(type: "mkdir", src: nil, dst: newPath)
            try await sftp.waitForJob(id: id)
        } catch {
            finalError = "フォルダ作成エラー: \(error.localizedDescription)"
        }
        await refreshThenReport(finalError.map { [$0] } ?? [], from: sftp)
    }

    func deleteFiles(_ files: [RemoteFile]) async {
        guard let sftp else { return }
        var queued: [QueuedOperation] = []
        var finalErrors: [String] = []
        for file in files {
            guard isPathAllowed(file.path) else {
                finalErrors.append("削除対象が許可範囲外です: \(file.path)")
                continue
            }
            do {
                let id = try await sftp.addToQueue(type: "delete", src: file.path, dst: nil)
                queued.append(QueuedOperation(id: id, name: file.name))
            } catch {
                finalErrors.append("削除登録エラー (\(file.name)): \(error.localizedDescription)")
            }
        }
        finalErrors.append(contentsOf: await waitForQueuedOperations(queued, from: sftp, actionName: "削除"))
        await refreshThenReport(finalErrors, from: sftp)
    }

    func renameFile(_ file: RemoteFile, to newName: String) async {
        guard let sftp else { return }
        guard isPathAllowed(file.path), let newPath = childPath(named: newName) else {
            reportOperationError("名前に /、.、.. は使用できません")
            return
        }
        var finalError: String?
        do {
            let id = try await sftp.addToQueue(type: "rename", src: file.path, dst: newPath)
            try await sftp.waitForJob(id: id)
        } catch {
            finalError = "名前変更エラー: \(error.localizedDescription)"
        }
        await refreshThenReport(finalError.map { [$0] } ?? [], from: sftp)
    }

    func copyFiles(_ files: [RemoteFile], to destination: String) async {
        await transferFiles(files, to: destination, type: "copy")
    }

    func moveFiles(_ files: [RemoteFile], to destination: String) async {
        await transferFiles(files, to: destination, type: "move")
    }

    private func transferFiles(_ files: [RemoteFile], to destination: String, type: String) async {
        guard let sftp else { return }
        guard isPathAllowed(destination) else {
            reportOperationError(type == "copy" ? "複製先が許可範囲外です" : "移動先が許可範囲外です")
            return
        }
        var queued: [QueuedOperation] = []
        var finalErrors: [String] = []
        for file in files {
            guard isPathAllowed(file.path) else {
                finalErrors.append("対象が許可範囲外です: \(file.path)")
                continue
            }
            let destinationPath = RemotePath.appending(file.name, to: destination)
            do {
                let id = try await sftp.addToQueue(type: type, src: file.path, dst: destinationPath)
                queued.append(QueuedOperation(id: id, name: file.name))
            } catch {
                finalErrors.append("登録エラー (\(file.name)): \(error.localizedDescription)")
            }
        }
        let actionName = type == "copy" ? "コピー" : "移動"
        finalErrors.append(contentsOf: await waitForQueuedOperations(queued, from: sftp, actionName: actionName))
        await refreshThenReport(finalErrors, from: sftp)
    }

    func changePermissions(_ files: [RemoteFile], mode: String) async {
        guard let sftp else { return }
        var queued: [QueuedOperation] = []
        var finalErrors: [String] = []
        for file in files {
            guard isPathAllowed(file.path) else {
                finalErrors.append("権限変更対象が許可範囲外です: \(file.path)")
                continue
            }
            do {
                let id = try await sftp.addToQueue(type: "chmod", src: nil, dst: file.path, mode: mode)
                queued.append(QueuedOperation(id: id, name: file.name))
            } catch {
                finalErrors.append("権限変更登録エラー (\(file.name)): \(error.localizedDescription)")
            }
        }
        finalErrors.append(contentsOf: await waitForQueuedOperations(queued, from: sftp, actionName: "権限変更"))
        await refreshThenReport(finalErrors, from: sftp)
    }

    @discardableResult
    private func loadPath(_ path: String, updateHistory: Bool) async -> Bool {
        // The toolbar disables itself while isLoading, but breadcrumb
        // navigation and double-click-to-open don't go through the toolbar
        // at all - each fires its own Task regardless of what's already in
        // flight. Guarding here, in the one place every navigation path
        // funnels through, covers all of them at once instead of needing
        // the same isLoading check duplicated at every UI entry point:
        // whichever call is already running keeps going, and a call that
        // arrives while it's in flight is simply dropped rather than
        // starting a second concurrent listDirectory against the same
        // connection.
        guard !isLoading else { return false }
        guard let sftp, isPathAllowed(path) else {
            if self.sftp != nil { reportOperationError("アクセスが許可されていません") }
            return false
        }

        navigationGeneration += 1
        let currentGeneration = navigationGeneration
        isLoading = true
        errors.setListingError(nil)
        do {
            let fileList = try await sftp.listDirectory(path: path)
            guard currentGeneration == navigationGeneration else { return false }

            if updateHistory {
                if pathHistoryIndex < pathHistory.count - 1 {
                    pathHistory = Array(pathHistory.prefix(pathHistoryIndex + 1))
                }
                pathHistory.append(path)
                pathHistoryIndex = pathHistory.count - 1
            }

            currentPath = path
            files = sortedVisibleFiles(fileList)
        } catch {
            guard currentGeneration == navigationGeneration else { return false }
            errors.setListingError(error.localizedDescription)
            isLoading = false
            return false
        }
        isLoading = false
        return true
    }

    private var currentSettings: Settings {
        Settings(showHiddenFiles: showHiddenFiles, refreshInterval: refreshInterval)
    }

    /// `didChangeNotification` fires for every defaults write, the saved server
    /// list included. Re-listing only when a setting this view model actually
    /// reads has changed keeps unrelated writes from triggering an SSH round trip.
    private func applySettingsChangeIfNeeded() async {
        let settings = currentSettings
        guard let previous = lastAppliedSettings else {
            lastAppliedSettings = settings
            return
        }
        guard settings != previous else { return }
        lastAppliedSettings = settings

        // Only the hidden-file setting changes which entries belong on screen,
        // and only a fresh listing can supply the ones being revealed. The
        // refresh interval just re-arms the timer - re-listing for it meant
        // dragging the slider fired one SSH round trip per step of the drag.
        if settings.showHiddenFiles != previous.showHiddenFiles {
            await refresh()
        } else {
            startAutoRefresh()
        }
    }

    /// Consecutive background-refresh failures tolerated before surfacing an
    /// error, mirroring SFTPService.pollFailuresTolerated - one dropped
    /// connection must not flash an error the very next tick clears on its
    /// own.
    private static let autoRefreshFailuresTolerated = 3

    private func startAutoRefresh() {
        refreshTask?.cancel()
        guard sftp != nil, refreshInterval.isFinite, refreshInterval > 0 else { return }
        let interval = min(refreshInterval, 300)
        let startGeneration = navigationGeneration

        refreshTask = Task { [weak self] in
            var consecutiveFailures = 0
            var hasReportedFailure = false
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(interval * 1_000_000_000))
                guard !Task.isCancelled else { break }

                // `self` is deliberately re-checked fresh each iteration
                // rather than bound once at the top of the loop and reused
                // across the long await below: holding a strong `self`
                // across it would keep this view model - and the SSH
                // process deinit's ssh?.terminateAll() exists to stop -
                // alive for however long listDirectory's 900s budget takes,
                // even after every external reference to it is gone.
                guard let snapshot = self.map({ (sftp: $0.sftp, path: $0.currentPath, generation: $0.navigationGeneration) }) else {
                    break // the view model itself is gone; nothing left to refresh
                }
                guard snapshot.generation == startGeneration else { break }
                guard let sftp = snapshot.sftp else { continue } // not connected (yet/anymore); keep waiting

                do {
                    let fileList = try await sftp.listDirectory(path: snapshot.path)
                    guard !Task.isCancelled, let self, self.navigationGeneration == startGeneration else { break }
                    self.files = self.sortedVisibleFiles(fileList)
                    consecutiveFailures = 0
                    hasReportedFailure = false
                } catch {
                    guard !Task.isCancelled, let self, self.navigationGeneration == startGeneration else { break }
                    // A background refresh used to swallow every failure and
                    // retry forever with nothing to show for it - a listing
                    // that quietly stopped updating looked identical to one
                    // that hadn't changed. Surface it once it's persistent
                    // rather than a single blip, the same tolerance
                    // SFTPService gives a single job's polling - but through
                    // the dismissible operation-error banner, not
                    // listingError: that slot replaces the whole file table
                    // with a full-page retry screen, which would hide an
                    // already-loaded, still-valid listing behind an error
                    // about a refresh that merely couldn't confirm it's
                    // still current. Reported at most once per failing
                    // streak, not on every poll past the threshold.
                    consecutiveFailures += 1
                    if consecutiveFailures > Self.autoRefreshFailuresTolerated, !hasReportedFailure {
                        hasReportedFailure = true
                        self.reportOperationError("バックグラウンド更新エラー: \(error.localizedDescription)")
                    }
                }
            }
        }
    }

    private func sortedVisibleFiles(_ fileList: [RemoteFile]) -> [RemoteFile] {
        let visible = showHiddenFiles ? fileList : fileList.filter { !$0.name.hasPrefix(".") }
        return visible.sorted { lhs, rhs in
            if lhs.isDirectory != rhs.isDirectory { return lhs.isDirectory }
            return lhs.name.localizedCaseInsensitiveCompare(rhs.name) == .orderedAscending
        }
    }

    private func isPathAllowed(_ path: String) -> Bool {
        guard ssh != nil else { return false }
        return RemotePath.isDescendant(path, of: remoteRoot)
    }

    private func childPath(named name: String) -> String? {
        guard RemotePath.isValidComponent(name) else {
            return nil
        }
        let path = RemotePath.appending(name, to: currentPath)
        return isPathAllowed(path) ? path : nil
    }

    /// Waits for every queued operation and turns each unsuccessful outcome
    /// into the same "<action>エラー (<name>): <message>" format the per-file
    /// loop this replaces used to build one entry at a time - just backed by
    /// SFTPService.waitForJobs' single shared poll instead of one waitForJob
    /// per file.
    private func waitForQueuedOperations(_ queued: [QueuedOperation], from sftp: SFTPService, actionName: String) async -> [String] {
        guard !queued.isEmpty else { return [] }
        do {
            let failures = try await sftp.waitForJobs(ids: queued.map(\.id))
            return queued.compactMap { operation in
                failures[operation.id].map { "\(actionName)エラー (\(operation.name)): \($0)" }
            }
        } catch {
            return queued.map { "\(actionName)エラー (\($0.name)): \(error.localizedDescription)" }
        }
    }

    /// Reports what an operation ran into, but only while the session it ran
    /// against is still the current one. Switching servers - or windows -
    /// mid-operation would otherwise surface errors about the old host next to
    /// the new host's file list, after disconnect() had already retired them.
    private func refreshThenReport(_ newErrors: [String], from service: SFTPService) async {
        guard service === sftp else { return }
        errors.addOperationErrors(newErrors)
        await refresh()
    }

    /// Not private: the view reports a few things it declines on its own -
    /// a drag with one entry it won't move alongside others it will - that
    /// never reach a ViewModel method at all.
    func reportOperationError(_ message: String) {
        errors.addOperationError(message)
    }

    /// Retires the operation errors. Called when the user navigates, reloads,
    /// or dismisses the banner — never by a background refresh, which would put
    /// back the disappearing-error bug these are separated to avoid.
    func dismissOperationErrors() {
        errors.clearOperationErrors()
    }

}
