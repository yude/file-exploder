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
    private var sshTasks: [Task<Void, Never>] = []
    
    private(set) var sftp: SFTPService?
    private var ssh: SSHConnection?
    
    var canGoBack: Bool {
        pathHistoryIndex > 0
    }
    
    var canGoForward: Bool {
        pathHistoryIndex < pathHistory.count - 1
    }
    
    func connect(server: Server) async {
        let sshConnection = SSHConnection(server: server)
        self.ssh = sshConnection
        self.sftp = SFTPService(ssh: sshConnection)
        
        // start observing setting changes before connecting to ensure clean state
        if let obs = settingsObserver {
            NotificationCenter.default.removeObserver(obs)
        }
        settingsObserver = NotificationCenter.default.addObserver(forName: UserDefaults.didChangeNotification, object: nil, queue: nil) { [weak self] _ in
            let task = Task { [weak self] in
                await self?.startAutoRefresh()
                await self?.refresh()
            }
            self?.sshTasks.append(task)
        }
        
        do {
            try await sshConnection.testConnection()
            await navigateTo(path: server.remoteRoot)
        } catch {
            errorMessage = error.localizedDescription
        }
    }
    
    func disconnect() {
        // 現在実行中のプロセスがあればキャンセルする
        self.ssh?.terminateAll()
        
        for task in sshTasks {
            task.cancel()
        }
        sshTasks.removeAll()
        
        refreshTask?.cancel()
        refreshTask = nil
        navigationGeneration += 1
        if let obs = settingsObserver {
            NotificationCenter.default.removeObserver(obs)
            settingsObserver = nil
        }
        
        self.ssh = nil
        self.sftp = nil
        self.files = []
        self.currentPath = "/"
        self.pathHistory = []
        self.pathHistoryIndex = -1
        self.errorMessage = nil
        self.isLoading = false
    }
    
    func navigateTo(path: String) async {
        // 接続サーバーのルートより上には行けないようにする
        if let serverRoot = ssh?.server.remoteRoot {
            let rootPath = (serverRoot as NSString).standardizingPath
            let targetPath = (path as NSString).standardizingPath
            if !targetPath.hasPrefix(rootPath) && targetPath != rootPath {
                return
            }
        }
        
        await loadPath(path, updateHistory: true)
        startAutoRefresh()
    }
    
    func goBack() async {
        guard canGoBack else { return }
        pathHistoryIndex -= 1
        let path = pathHistory[pathHistoryIndex]
        await loadPath(path, updateHistory: false)
    }
    
    func goForward() async {
        guard canGoForward else { return }
        pathHistoryIndex += 1
        let path = pathHistory[pathHistoryIndex]
        await loadPath(path, updateHistory: false)
    }
    
    func goToParent() async {
        let parent = (currentPath as NSString).deletingLastPathComponent
        guard parent != currentPath else { return }
        
        // 接続サーバーのルートより上には行けないようにする
        if let serverRoot = ssh?.server.remoteRoot {
            let rootPath = (serverRoot as NSString).standardizingPath
            let targetPath = (parent as NSString).standardizingPath
            if !targetPath.hasPrefix(rootPath) && targetPath != rootPath {
                return
            }
        }
        
        await navigateTo(path: parent)
    }
    
    func refresh() async {
        await loadPath(currentPath, updateHistory: false)
    }
    
    func openFile(_ file: RemoteFile) async {
        if file.isDirectory {
            await navigateTo(path: file.path)
        }
    }
    
    func createFolder(name: String) async {
        guard let sftp = sftp else { return }
        let newPath = "\(currentPath)/\(name)"
        var finalError: String?
        do {
            let id = try await sftp.addToQueue(type: "mkdir", src: nil, dst: newPath)
            try await sftp.waitForJob(id: id)
        } catch {
            finalError = "フォルダ作成エラー: \(error.localizedDescription)"
        }
        if let err = finalError {
            errorMessage = err
        }
        await refresh()
    }
    
    func deleteFiles(_ files: [RemoteFile]) async {
        guard let sftp = sftp else { return }
        var waitIds: [String] = []
        var finalErrors: [String] = []
        for file in files {
            do {
                let id = try await sftp.addToQueue(type: "delete", src: file.path, dst: nil)
                waitIds.append(id)
            } catch {
                finalErrors.append("削除登録エラー (\(file.name)): \(error.localizedDescription)")
            }
        }
        for id in waitIds {
            do {
                try await sftp.waitForJob(id: id)
            } catch {
                finalErrors.append("削除エラー: \(error.localizedDescription)")
            }
        }
        if !finalErrors.isEmpty {
            errorMessage = finalErrors.joined(separator: "\n")
        }
        await refresh()
    }
    
    func renameFile(_ file: RemoteFile, to newName: String) async {
        guard let sftp = sftp else { return }
        let newPath = "\(currentPath)/\(newName)"
        var finalError: String?
        do {
            let id = try await sftp.addToQueue(type: "rename", src: file.path, dst: newPath)
            try await sftp.waitForJob(id: id)
        } catch {
            finalError = "名前変更エラー: \(error.localizedDescription)"
        }
        if let err = finalError {
            errorMessage = err
        }
        await refresh()
    }
    
    func copyFiles(_ files: [RemoteFile], to destination: String) async {
        guard let sftp = sftp else { return }
        var waitIds: [String] = []
        var finalErrors: [String] = []
        for file in files {
            let destPath = "\(destination)/\(file.name)"
            do {
                let id = try await sftp.addToQueue(type: "copy", src: file.path, dst: destPath)
                waitIds.append(id)
            } catch {
                finalErrors.append("コピー登録エラー (\(file.name)): \(error.localizedDescription)")
            }
        }
        for id in waitIds {
            do {
                try await sftp.waitForJob(id: id, timeout: 30.0)
            } catch {
                finalErrors.append("コピーエラー: \(error.localizedDescription)")
            }
        }
        if !finalErrors.isEmpty {
            errorMessage = finalErrors.joined(separator: "\n")
        }
        await refresh()
    }
    
    func moveFiles(_ files: [RemoteFile], to destination: String) async {
        guard let sftp = sftp else { return }
        var waitIds: [String] = []
        var finalErrors: [String] = []
        for file in files {
            let destPath = "\(destination)/\(file.name)"
            do {
                let id = try await sftp.addToQueue(type: "move", src: file.path, dst: destPath)
                waitIds.append(id)
            } catch {
                finalErrors.append("移動登録エラー (\(file.name)): \(error.localizedDescription)")
            }
        }
        for id in waitIds {
            do {
                try await sftp.waitForJob(id: id, timeout: 30.0)
            } catch {
                finalErrors.append("移動エラー: \(error.localizedDescription)")
            }
        }
        if !finalErrors.isEmpty {
            errorMessage = finalErrors.joined(separator: "\n")
        }
        await refresh()
    }
    
    func changePermissions(_ file: RemoteFile, mode: String) async {
        guard let sftp = sftp else { return }
        var finalError: String?
        do {
            let id = try await sftp.addToQueue(type: "chmod", src: nil, dst: file.path, mode: mode)
            try await sftp.waitForJob(id: id)
        } catch {
            finalError = "権限変更エラー (\(file.name)): \(error.localizedDescription)"
        }
        if let err = finalError {
            errorMessage = err
        }
        await refresh()
    }
    
    private func loadPath(_ path: String, updateHistory: Bool) async {
        guard let sftp = sftp else { return }
        
        navigationGeneration += 1
        let currentGeneration = navigationGeneration
        
        isLoading = true
        errorMessage = nil
        do {
            let fileList = try await sftp.listDirectory(path: path)
            guard currentGeneration == navigationGeneration else { return }
            
            if updateHistory {
                if pathHistoryIndex < pathHistory.count - 1 {
                    pathHistory = Array(pathHistory.prefix(pathHistoryIndex + 1))
                }
                pathHistory.append(path)
                pathHistoryIndex = pathHistory.count - 1
            }
            
            currentPath = path
            let visibleFiles = showHiddenFiles ? fileList : fileList.filter { !$0.name.hasPrefix(".") }
            files = visibleFiles.sorted { lhs, rhs in
                if lhs.isDirectory != rhs.isDirectory {
                    return lhs.isDirectory
                }
                return lhs.name.localizedCaseInsensitiveCompare(rhs.name) == .orderedAscending
            }
        } catch {
            guard currentGeneration == navigationGeneration else { return }
            errorMessage = error.localizedDescription
        }
        
        if currentGeneration == navigationGeneration {
            isLoading = false
        }
    }
    
    private func startAutoRefresh() {
        refreshTask?.cancel()
        guard refreshInterval > 0 else { return }
        
        let startGeneration = navigationGeneration
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self = self else { break }
                try? await Task.sleep(nanoseconds: UInt64(self.refreshInterval * 1_000_000_000))
                guard !Task.isCancelled else { break }
                guard startGeneration == self.navigationGeneration else { break }
                
                // Background refresh without showing loading indicator
                if let sftp = self.sftp {
                    let currentPath = self.currentPath
                    if let fileList = try? await sftp.listDirectory(path: currentPath) {
                        guard !Task.isCancelled else { break }
                        guard startGeneration == self.navigationGeneration else { break }
                        let showHidden = self.showHiddenFiles
                        let visibleFiles = showHidden ? fileList : fileList.filter { !$0.name.hasPrefix(".") }
                        self.files = visibleFiles.sorted { lhs, rhs in
                            if lhs.isDirectory != rhs.isDirectory { return lhs.isDirectory }
                            return lhs.name.localizedCaseInsensitiveCompare(rhs.name) == .orderedAscending
                        }
                    }
                }
            }
        }
    }
}
