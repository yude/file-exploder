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
        } catch {
            errorMessage = error.localizedDescription
        }
    }
    
    func disconnect() {
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
        guard let sftp = sftp else { return }
        
        isLoading = true
        errorMessage = nil
        
        do {
            let fileList = try await sftp.listDirectory(path: path)
            
            // Update history
            if pathHistoryIndex < pathHistory.count - 1 {
                pathHistory = Array(pathHistory.prefix(pathHistoryIndex + 1))
            }
            pathHistory.append(path)
            pathHistoryIndex = pathHistory.count - 1
            
            currentPath = path
            files = fileList.sorted { lhs, rhs in
                // Directories first, then by name
                if lhs.isDirectory != rhs.isDirectory {
                    return lhs.isDirectory
                }
                return lhs.name.localizedCaseInsensitiveCompare(rhs.name) == .orderedAscending
            }
        } catch {
            errorMessage = error.localizedDescription
        }
        
        isLoading = false
    }
    
    func goBack() async {
        guard canGoBack else { return }
        pathHistoryIndex -= 1
        let path = pathHistory[pathHistoryIndex]
        await loadPath(path)
    }
    
    func goForward() async {
        guard canGoForward else { return }
        pathHistoryIndex += 1
        let path = pathHistory[pathHistoryIndex]
        await loadPath(path)
    }
    
    func goToParent() async {
        let parent = (currentPath as NSString).deletingLastPathComponent
        await navigateTo(path: parent)
    }
    
    func refresh() async {
        await loadPath(currentPath)
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
    
    private func loadPath(_ path: String) async {
        guard let sftp = sftp else { return }
        
        isLoading = true
        errorMessage = nil
        do {
            let fileList = try await sftp.listDirectory(path: path)
            currentPath = path
            files = fileList.sorted { lhs, rhs in
                if lhs.isDirectory != rhs.isDirectory {
                    return lhs.isDirectory
                }
                return lhs.name.localizedCaseInsensitiveCompare(rhs.name) == .orderedAscending
            }
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }
}
