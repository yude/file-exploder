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
        
        do {
            try await sshConnection.testConnection()
            await navigateTo(path: server.remoteRoot)
            
            // start observing setting changes
            NotificationCenter.default.addObserver(forName: UserDefaults.didChangeNotification, object: nil, queue: nil) { [weak self] _ in
                Task { [weak self] in
                    await self?.startAutoRefresh()
                    await self?.refresh()
                }
            }
            
        } catch {
            errorMessage = error.localizedDescription
        }
    }
    
    func disconnect() {
        // 現在実行中のプロセスがあればキャンセルする
        if let process = self.ssh?.process, process.isRunning {
            process.terminate()
        }
        refreshTask?.cancel()
        refreshTask = nil
        navigationGeneration += 1
        NotificationCenter.default.removeObserver(self)
        
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
        do {
            let id = try await sftp.addToQueue(type: "mkdir", src: nil, dst: newPath)
            try await sftp.waitForJob(id: id)
            await refresh()
        } catch {
            errorMessage = "フォルダ作成エラー: \(error.localizedDescription)"
        }
    }
    
    func deleteFiles(_ files: [RemoteFile]) async {
        guard let sftp = sftp else { return }
        var waitIds: [String] = []
        for file in files {
            do {
                let id = try await sftp.addToQueue(type: "delete", src: file.path, dst: nil)
                waitIds.append(id)
            } catch {
                errorMessage = "削除エラー (\(file.name)): \(error.localizedDescription)"
            }
        }
        for id in waitIds {
            try? await sftp.waitForJob(id: id)
        }
        await refresh()
    }
    
    func renameFile(_ file: RemoteFile, to newName: String) async {
        guard let sftp = sftp else { return }
        let newPath = "\(currentPath)/\(newName)"
        do {
            let id = try await sftp.addToQueue(type: "rename", src: file.path, dst: newPath)
            try await sftp.waitForJob(id: id)
            await refresh()
        } catch {
            errorMessage = "名前変更エラー: \(error.localizedDescription)"
        }
    }
    
    func copyFiles(_ files: [RemoteFile], to destination: String) async {
        guard let sftp = sftp else { return }
        var waitIds: [String] = []
        for file in files {
            let destPath = "\(destination)/\(file.name)"
            do {
                let id = try await sftp.addToQueue(type: "copy", src: file.path, dst: destPath)
                waitIds.append(id)
            } catch {
                errorMessage = "コピーエラー (\(file.name)): \(error.localizedDescription)"
            }
        }
        for id in waitIds {
            try? await sftp.waitForJob(id: id, timeout: 30.0)
        }
        await refresh()
    }
    
    func moveFiles(_ files: [RemoteFile], to destination: String) async {
        guard let sftp = sftp else { return }
        var waitIds: [String] = []
        for file in files {
            let destPath = "\(destination)/\(file.name)"
            do {
                let id = try await sftp.addToQueue(type: "move", src: file.path, dst: destPath)
                waitIds.append(id)
            } catch {
                errorMessage = "移動エラー (\(file.name)): \(error.localizedDescription)"
            }
        }
        for id in waitIds {
            try? await sftp.waitForJob(id: id, timeout: 30.0)
        }
        await refresh()
    }
    
    func changePermissions(_ file: RemoteFile, mode: String) async {
        guard let sftp = sftp else { return }
        do {
            let id = try await sftp.addToQueue(type: "chmod", src: nil, dst: file.path, mode: mode)
            try await sftp.waitForJob(id: id)
            await refresh()
        } catch {
            errorMessage = "権限変更エラー (\(file.name)): \(error.localizedDescription)"
        }
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
        
        refreshTask = Task {
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(refreshInterval * 1_000_000_000))
                guard !Task.isCancelled else { break }
                
                // Background refresh without showing loading indicator
                if let sftp = self.sftp {
                    if let fileList = try? await sftp.listDirectory(path: self.currentPath) {
                        let visibleFiles = self.showHiddenFiles ? fileList : fileList.filter { !$0.name.hasPrefix(".") }
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
