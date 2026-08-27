import Foundation
import SwiftUI

@MainActor
class FileListViewModel: ObservableObject {
    @Published var currentPath: String = "/"
    @Published var files: [RemoteFile] = []
    @Published var isLoading = false
    @Published var errorMessage: String?
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
                errorMessage = message
            }
        } catch {
            guard ssh === sshConnection else { return }
            let message = error.localizedDescription
            disconnect()
            errorMessage = message
        }
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
        errorMessage = nil
        isLoading = false
    }

    func navigateTo(path: String) async {
        guard isPathAllowed(path) else {
            errorMessage = "アクセスが許可されていません"
            return
        }
        if await loadPath(path, updateHistory: true) {
            startAutoRefresh()
        }
    }

    func goBack() async {
        guard canGoBack else { return }
        let newIndex = pathHistoryIndex - 1
        if await loadPath(pathHistory[newIndex], updateHistory: false) {
            pathHistoryIndex = newIndex
            startAutoRefresh()
        }
    }

    func goForward() async {
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

    func openFile(_ file: RemoteFile) async {
        if file.isDirectory {
            await navigateTo(path: file.path)
        }
    }

    func createFolder(name: String) async {
        guard let sftp, let newPath = childPath(named: name) else {
            errorMessage = "フォルダ名に /、.、.. は使用できません"
            return
        }
        var finalError: String?
        do {
            let id = try await sftp.addToQueue(type: "mkdir", src: nil, dst: newPath)
            try await sftp.waitForJob(id: id)
        } catch {
            finalError = "フォルダ作成エラー: \(error.localizedDescription)"
        }
        await refreshThenReport(finalError.map { [$0] } ?? [])
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
        for operation in queued {
            do {
                try await sftp.waitForJob(id: operation.id)
            } catch {
                finalErrors.append("削除エラー (\(operation.name)): \(error.localizedDescription)")
            }
        }
        await refreshThenReport(finalErrors)
    }

    func renameFile(_ file: RemoteFile, to newName: String) async {
        guard let sftp else { return }
        guard isPathAllowed(file.path), let newPath = childPath(named: newName) else {
            errorMessage = "名前に /、.、.. は使用できません"
            return
        }
        var finalError: String?
        do {
            let id = try await sftp.addToQueue(type: "rename", src: file.path, dst: newPath)
            try await sftp.waitForJob(id: id)
        } catch {
            finalError = "名前変更エラー: \(error.localizedDescription)"
        }
        await refreshThenReport(finalError.map { [$0] } ?? [])
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
            errorMessage = type == "copy" ? "複製先が許可範囲外です" : "移動先が許可範囲外です"
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
        for operation in queued {
            do {
                try await sftp.waitForJob(id: operation.id)
            } catch {
                finalErrors.append("\(actionName)エラー (\(operation.name)): \(error.localizedDescription)")
            }
        }
        await refreshThenReport(finalErrors)
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
        for operation in queued {
            do {
                try await sftp.waitForJob(id: operation.id)
            } catch {
                finalErrors.append("権限変更エラー (\(operation.name)): \(error.localizedDescription)")
            }
        }
        await refreshThenReport(finalErrors)
    }

    @discardableResult
    private func loadPath(_ path: String, updateHistory: Bool) async -> Bool {
        guard let sftp, isPathAllowed(path) else {
            if self.sftp != nil { errorMessage = "アクセスが許可されていません" }
            return false
        }

        navigationGeneration += 1
        let currentGeneration = navigationGeneration
        isLoading = true
        errorMessage = nil
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
            errorMessage = error.localizedDescription
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
        guard settings != lastAppliedSettings else { return }
        lastAppliedSettings = settings
        await refresh()
    }

    private func startAutoRefresh() {
        refreshTask?.cancel()
        guard sftp != nil, refreshInterval.isFinite, refreshInterval > 0 else { return }
        let interval = min(refreshInterval, 300)
        let startGeneration = navigationGeneration

        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { break }
                try? await Task.sleep(nanoseconds: UInt64(interval * 1_000_000_000))
                guard !Task.isCancelled, startGeneration == self.navigationGeneration else { break }
                if let sftp = self.sftp {
                    let path = self.currentPath
                    if let fileList = try? await sftp.listDirectory(path: path) {
                        guard !Task.isCancelled, startGeneration == self.navigationGeneration else { break }
                        self.files = self.sortedVisibleFiles(fileList)
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
        guard !name.isEmpty, name != ".", name != "..", !name.contains("/"), !name.contains("\0") else {
            return nil
        }
        let path = RemotePath.appending(name, to: currentPath)
        return isPathAllowed(path) ? path : nil
    }

    private func refreshThenReport(_ operationErrors: [String]) async {
        await refresh()
        guard !operationErrors.isEmpty else { return }
        var errors = operationErrors
        if let refreshError = errorMessage {
            errors.append("一覧更新エラー: \(refreshError)")
        }
        errorMessage = errors.joined(separator: "\n")
    }
}
