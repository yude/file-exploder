import Foundation

/// SFTP service for directory listing and file operations
class SFTPService {
    private let ssh: SSHConnection
    private let commandPrefix = "PATH=\"$PATH:/usr/local/bin:$HOME/.local/bin\"; export PATH; file-exploder"
    
    init(ssh: SSHConnection) {
        self.ssh = ssh
    }
    
    /// List directory contents
    func listDirectory(path: String) async throws -> [RemoteFile] {
        // Use the new go server list command
        let command = "\(commandPrefix) list -- \(path.shellEscaped)"
        let output = try await ssh.executeCommand(command)
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        let list = try parseJSON(data, type: [RemoteFileJSON].self)
        
        return list.map(makeRemoteFile)
    }
    
    /// Get file info
    func getFileInfo(path: String) async throws -> RemoteFile {
        // Use the new go server stat command
        let command = "\(commandPrefix) stat -- \(path.shellEscaped)"
        let output = try await ssh.executeCommand(command)
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        let item = try parseJSON(data, type: RemoteFileJSON.self)
        
        return makeRemoteFile(item)
    }
    
    /// Create directory
    func createDirectory(path: String) async throws {
        _ = try await addToQueue(type: "mkdir", src: nil, dst: path)
    }
    
    /// Delete file or directory
    func delete(path: String, recursive: Bool = false) async throws {
        _ = try await addToQueue(type: "delete", src: path, dst: nil)
    }
    
    /// Rename/move
    func rename(source: String, destination: String) async throws {
        _ = try await addToQueue(type: "rename", src: source, dst: destination)
    }
    
    /// Copy
    func copy(source: String, destination: String, recursive: Bool = false) async throws {
        _ = try await addToQueue(type: "copy", src: source, dst: destination)
    }
    
    /// Change permissions
    func chmod(path: String, mode: String) async throws {
        _ = try await addToQueue(type: "chmod", src: nil, dst: path, mode: mode)
    }
    
    /// Add operation to server-side queue
    func addToQueue(type: String, src: String?, dst: String?, mode: String? = nil) async throws -> String {
        var command = "\(commandPrefix) add --type \(type)"
        if let src = src {
            command += " --src \(src.shellEscaped)"
        }
        if let dst = dst {
            command += " --dst \(dst.shellEscaped)"
        }
        if let mode = mode {
            command += " --mode \(mode.shellEscaped)"
        }
        let output = try await ssh.executeCommand(command)
        // Parse JSON response {"id":"xxx","status":"pending"}
        guard let data = output.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = json["id"] as? String else {
            throw QueueError.invalidResponse("不正なジョブ登録レスポンス: \(output)")
        }
        return id
    }
    
    /// Get queue status
    func getQueueStatus() async throws -> [QueueJob] {
        let output = try await ssh.executeCommand("\(commandPrefix) status")
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        let response = try parseJSON(data, type: QueueResponse.self)
        return response.jobs ?? []
    }
    
    /// Get recent job logs
    func getJobLogs(limit: Int = 50) async throws -> [QueueJob] {
        let output = try await ssh.executeCommand("\(commandPrefix) log --limit \(limit)")
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        return try parseJSON(data, type: [QueueJob].self)
    }
    
    /// Get specific job status
    func getJobStatus(id: String) async throws -> QueueJob {
        let output = try await ssh.executeCommand("\(commandPrefix) status -- \(id.shellEscaped)")
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        return try parseJSON(data, type: QueueJob.self)
    }
    
    /// Wait for a queued operation. With no timeout, polling ends on completion,
    /// an SSH failure, or task cancellation; long-running copies are not reported
    /// as failed merely because they exceeded an arbitrary UI deadline.
    func waitForJob(id: String, timeout: TimeInterval? = nil) async throws {
        let start = Date()
        while timeout == nil || Date().timeIntervalSince(start) < timeout! {
            try Task.checkCancellation()
            let job = try await getJobStatus(id: id)
            switch job.status {
            case .completed:
                return
            case .failed:
                throw QueueError.invalidResponse(job.error ?? "Unknown error")
            case .cancelled:
                throw QueueError.invalidResponse("ジョブがキャンセルされました")
            case .pending, .running:
                break
            }
            try await Task.sleep(nanoseconds: 500_000_000)
        }
        throw QueueError.invalidResponse("処理がタイムアウトしました。ジョブ画面で状態を確認してください")
    }
    
    /// Cancel a queue job
    func cancelJob(id: String) async throws {
        _ = try await ssh.executeCommand("\(commandPrefix) cancel -- \(id.shellEscaped)")
    }
    
    // MARK: - Private helpers

    private func makeRemoteFile(_ item: RemoteFileJSON) -> RemoteFile {
        RemoteFile(
            name: item.name,
            path: item.path,
            size: item.size,
            modificationDate: Date(timeIntervalSince1970: TimeInterval(item.modificationDate)),
            isDirectory: item.isDirectory,
            isSymlink: item.isSymlink ?? false,
            permissions: FilePermissions.from(octal: Int(item.permissions))
        )
    }
    
    private func parseJSON<T: Decodable>(_ data: Data, type: T.Type) throws -> T {
        let decoder = JSONDecoder.fileExploderDecoder()
        do {
            return try decoder.decode(T.self, from: data)
        } catch {
            if let rawString = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines), !rawString.isEmpty {
                throw QueueError.invalidResponse("レスポンスの解析に失敗しました: \(error.localizedDescription)\nサーバー応答: \(rawString)")
            }
            throw QueueError.invalidResponse("レスポンスの解析に失敗しました: \(error.localizedDescription)")
        }
    }
}

// MARK: - Supporting types

struct QueueResponse: Codable {
    let total: Int
    let jobs: [QueueJob]?
}

struct RemoteFileJSON: Codable {
    let name: String
    let path: String
    let size: Int64
    let modificationDate: Int64
    let isDirectory: Bool
    /// Optional so the client keeps working against daemons predating the field.
    let isSymlink: Bool?
    let permissions: UInt32
}

enum QueueError: LocalizedError {
    case invalidResponse(String)
    case jobNotFound
    
    var errorDescription: String? {
        switch self {
        case .invalidResponse(let msg):
            return msg
        case .jobNotFound:
            return "Job not found"
        }
    }
}
