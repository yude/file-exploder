import Foundation

/// SFTP service for directory listing and file operations
class SFTPService {
    private let ssh: SSHConnection
    
    init(ssh: SSHConnection) {
        self.ssh = ssh
    }
    
    /// List directory contents
    func listDirectory(path: String) async throws -> [RemoteFile] {
        // Use the new go server list command
        let command = "PATH=$PATH:/usr/local/bin:~/.local/bin file-exploder list \(path.shellEscaped)"
        let output = try await ssh.executeCommand(command)
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        let list = try parseJSON(data, type: [RemoteFileJSON].self)
        
        return list.map { item in
            let date = Date(timeIntervalSince1970: TimeInterval(item.modificationDate))
            var perms = FilePermissions.from(octal: Int(item.permissions))
            perms.isDirectory = item.isDirectory
            return RemoteFile(
                name: item.name,
                path: item.path,
                size: item.size,
                modificationDate: date,
                isDirectory: item.isDirectory,
                permissions: perms
            )
        }
    }
    
    /// Get file info
    func getFileInfo(path: String) async throws -> RemoteFile {
        // Use the new go server stat command
        let command = "PATH=$PATH:/usr/local/bin:~/.local/bin file-exploder stat \(path.shellEscaped)"
        let output = try await ssh.executeCommand(command)
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        let item = try parseJSON(data, type: RemoteFileJSON.self)
        
        let date = Date(timeIntervalSince1970: TimeInterval(item.modificationDate))
        var perms = FilePermissions.from(octal: Int(item.permissions))
        perms.isDirectory = item.isDirectory
        return RemoteFile(
            name: item.name,
            path: item.path,
            size: item.size,
            modificationDate: date,
            isDirectory: item.isDirectory,
            permissions: perms
        )
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
        var command = "PATH=$PATH:/usr/local/bin:~/.local/bin file-exploder add --type \(type)"
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
        let output = try await ssh.executeCommand("PATH=$PATH:/usr/local/bin:~/.local/bin file-exploder status")
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        let response = try parseJSON(data, type: QueueResponse.self)
        return response.jobs ?? []
    }
    
    /// Get recent job logs
    func getJobLogs(limit: Int = 50) async throws -> [QueueJob] {
        let output = try await ssh.executeCommand("PATH=$PATH:/usr/local/bin:~/.local/bin file-exploder log --limit \(limit)")
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        return try parseJSON(data, type: [QueueJob].self)
    }
    
    /// Get specific job status
    func getJobStatus(id: String) async throws -> QueueJob {
        let output = try await ssh.executeCommand("PATH=$PATH:/usr/local/bin:~/.local/bin file-exploder status \(id.shellEscaped)")
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        return try parseJSON(data, type: QueueJob.self)
    }
    
    /// Wait for job to complete
    func waitForJob(id: String, timeout: TimeInterval = 60.0) async throws {
        let start = Date()
        while Date().timeIntervalSince(start) < timeout {
            let job = try await getJobStatus(id: id)
            if job.status == .completed {
                return
            } else if job.status == .failed {
                throw QueueError.invalidResponse(job.error ?? "Unknown error")
            } else if job.status == .cancelled {
                throw QueueError.invalidResponse("ジョブがキャンセルされました")
            }
            try? await Task.sleep(nanoseconds: 500_000_000) // 0.5s
            if Task.isCancelled {
                throw QueueError.invalidResponse("処理が中断されました")
            }
        }
        throw QueueError.invalidResponse("処理がタイムアウトしました")
    }
    
    /// Cancel a queue job
    func cancelJob(id: String) async throws {
        _ = try await ssh.executeCommand("PATH=$PATH:/usr/local/bin:~/.local/bin file-exploder cancel \(id.shellEscaped)")
    }
    
    // MARK: - Private helpers
    
    private func parseJSON<T: Decodable>(_ data: Data, type: T.Type) throws -> T {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
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
