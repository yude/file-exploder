import Foundation

/// SFTP service for directory listing and file operations
class SFTPService {
    private let ssh: SSHConnection
    
    init(ssh: SSHConnection) {
        self.ssh = ssh
    }
    
    /// List directory contents
    func listDirectory(path: String) async throws -> [RemoteFile] {
        // Use find for robust parseable output: size|timestamp|octal_mode|type|name
        // Name is last so maxSplits correctly captures filenames containing '|'
        let command = "find \(path.shellEscaped) -maxdepth 1 -mindepth 1 -printf '%s|%Ts|%m|%y|%f\\n'"
        let output = try await ssh.executeCommand(command)
        
        return parseDirectoryListing(output, basePath: path)
    }
    
    /// Get file info
    func getFileInfo(path: String) async throws -> RemoteFile {
        let command = "find \(path.shellEscaped) -maxdepth 0 -printf '%s|%Ts|%m|%y|%f\\n'"
        let output = try await ssh.executeCommand(command)
        guard let file = parseFindLine(output.trimmingCharacters(in: .whitespacesAndNewlines), basePath: (path as NSString).deletingLastPathComponent) else {
            throw SSHError.invalidResponse
        }
        return file
    }
    
    /// Create directory
    func createDirectory(path: String) async throws {
        _ = try await ssh.executeCommand("mkdir -p \(path.shellEscaped)")
    }
    
    /// Delete file or directory
    func delete(path: String, recursive: Bool = false) async throws {
        let flag = recursive ? "-rf" : "-f"
        _ = try await ssh.executeCommand("rm \(flag) \(path.shellEscaped)")
    }
    
    /// Rename/move
    func rename(source: String, destination: String) async throws {
        _ = try await ssh.executeCommand("mv \(source.shellEscaped) \(destination.shellEscaped)")
    }
    
    /// Copy
    func copy(source: String, destination: String, recursive: Bool = false) async throws {
        let flag = recursive ? "-r" : ""
        _ = try await ssh.executeCommand("cp \(flag) \(source.shellEscaped) \(destination.shellEscaped)")
    }
    
    /// Change permissions
    func chmod(path: String, mode: String) async throws {
        _ = try await ssh.executeCommand("chmod \(mode) \(path.shellEscaped)")
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
    func waitForJob(id: String, timeout: TimeInterval = 10.0) async throws {
        let start = Date()
        while Date().timeIntervalSince(start) < timeout {
            let job = try await getJobStatus(id: id)
            if job.status == .completed {
                return
            } else if job.status == .failed {
                throw QueueError.invalidResponse(job.error ?? "Unknown error")
            } else if job.status == .cancelled {
                throw QueueError.invalidResponse("Job was cancelled")
            }
            try await Task.sleep(nanoseconds: 500_000_000) // 0.5s
        }
        throw QueueError.invalidResponse("Timeout waiting for job completion")
    }
    
    /// Cancel a queue job
    func cancelJob(id: String) async throws {
        _ = try await ssh.executeCommand("PATH=$PATH:/usr/local/bin:~/.local/bin file-exploder cancel \(id.shellEscaped)")
    }
    
    // MARK: - Private helpers
    
    private func parseDirectoryListing(_ output: String, basePath: String) -> [RemoteFile] {
        let lines = output.components(separatedBy: .newlines).filter { !$0.isEmpty }
        var files: [RemoteFile] = []
        
        for line in lines {
            guard let file = parseFindLine(line, basePath: basePath) else { continue }
            files.append(file)
        }
        
        return files
    }
    
    private func parseFindLine(_ line: String, basePath: String) -> RemoteFile? {
        let parts = line.split(separator: "|", maxSplits: 4)
        guard parts.count >= 5 else { return nil }
        
        let size = Int64(parts[0]) ?? 0
        let timestamp = TimeInterval(parts[1]) ?? 0
        let mode = Int(parts[2], radix: 8) ?? 0
        let type = String(parts[3])
        let name = String(parts[4])
        
        // Skip . and .. just in case, though find -mindepth 1 prevents this
        guard name != "." && name != ".." else { return nil }
        
        let isDirectory = (type == "d")
        let date = Date(timeIntervalSince1970: timestamp)
        let filePermissions = FilePermissions.from(octal: mode)
        let filePath = "\(basePath)/\(name)".replacingOccurrences(of: "//", with: "/")
        
        return RemoteFile(
            name: name,
            path: filePath,
            size: size,
            modificationDate: date,
            isDirectory: isDirectory,
            permissions: filePermissions
        )
    }
    

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
