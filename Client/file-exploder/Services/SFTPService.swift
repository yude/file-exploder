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
        // Keep the path ASCII until it reaches the Go process. Passing a
        // composed Linux filename directly through macOS Process can turn it
        // into a canonically equivalent but byte-distinct path.
        let command = "\(commandPrefix) list --path-base64 \(path.utf8Base64.shellEscaped)"
        let legacyCommand = "\(commandPrefix) list -- \(path.shellEscaped)"
        let output = try await executeWithLegacyFallback(
            command,
            legacyCommand: legacyCommand,
            unsupportedFlags: ["--path-base64"]
        )
        guard let data = output.data(using: .utf8) else {
            throw QueueError.invalidResponse("Empty response")
        }
        let list = try parseJSON(data, type: [RemoteFileJSON].self)
        
        return list.map(makeRemoteFile)
    }
    
    /// Add operation to server-side queue
    func addToQueue(type: String, src: String?, dst: String?, mode: String? = nil) async throws -> String {
        var command = "\(commandPrefix) add --type \(type)"
        var legacyCommand = command
        if let src = src {
            command += " --src-base64 \(src.utf8Base64.shellEscaped)"
            legacyCommand += " --src \(src.shellEscaped)"
        }
        if let dst = dst {
            command += " --dst-base64 \(dst.utf8Base64.shellEscaped)"
            legacyCommand += " --dst \(dst.shellEscaped)"
        }
        if let mode = mode {
            command += " --mode \(mode.shellEscaped)"
            legacyCommand += " --mode \(mode.shellEscaped)"
        }
        let output = try await executeWithLegacyFallback(
            command,
            legacyCommand: legacyCommand,
            unsupportedFlags: ["--src-base64", "--dst-base64"]
        )
        // Parse JSON response {"id":"xxx","status":"pending"}
        guard let data = output.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = json["id"] as? String else {
            throw QueueError.invalidResponse("不正なジョブ登録レスポンス: \(output.truncatedForDisplay)")
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
    
    /// How long a job may sit pending, with nothing else running, before the
    /// queue is treated as stalled.
    private static let stalledQueueGracePeriod: TimeInterval = 30

    /// Wait for a queued operation. Once a job is running the wait is unbounded:
    /// a large copy legitimately takes minutes and must not be reported as
    /// failed for missing an arbitrary UI deadline.
    ///
    /// A job that never even starts is a different story. If the daemon is not
    /// running - the case the README's `loginctl enable-linger` note is about -
    /// the job stays pending forever and every operation used to hang behind a
    /// spinner with nothing to act on. Give up only once the job has waited out
    /// the grace period *and* the server reports nothing running at all, twice
    /// in a row, so a busy queue is never mistaken for a dead one.
    func waitForJob(id: String, timeout: TimeInterval? = nil) async throws {
        let start = Date()
        // Every poll opens its own SSH connection, so start snappy for the
        // common quick operation and back off for copies that run for minutes.
        var pollInterval: UInt64 = 300_000_000
        let maxPollInterval: UInt64 = 3_000_000_000
        var pendingSince = Date()
        var everStarted = false
        var stalledObservations = 0

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
            case .running:
                everStarted = true
            case .pending:
                if !everStarted, Date().timeIntervalSince(pendingSince) > Self.stalledQueueGracePeriod {
                    if try await queueIsMoving() {
                        pendingSince = Date()
                        stalledObservations = 0
                    } else {
                        stalledObservations += 1
                        if stalledObservations >= 2 {
                            throw QueueError.invalidResponse(
                                "ジョブが開始されないままです。サーバーで file-exploder デーモンが動作しているか確認してください "
                                + "(systemctl --user status file-exploder)。ジョブはキューに残っています。"
                            )
                        }
                    }
                }
            }
            try await Task.sleep(nanoseconds: pollInterval)
            pollInterval = min(pollInterval * 2, maxPollInterval)
        }
        throw QueueError.invalidResponse("処理がタイムアウトしました。ジョブ画面で状態を確認してください")
    }

    private func queueIsMoving() async throws -> Bool {
        try await getQueueStatus().contains { $0.status == .running }
    }
    
    /// Cancel a queue job
    func cancelJob(id: String) async throws {
        _ = try await ssh.executeCommand("\(commandPrefix) cancel -- \(id.shellEscaped)")
    }
    
    // MARK: - Private helpers

    /// Keep a new client usable with an older server. Cobra rejects unknown
    /// flags before running the command, so retrying only this exact error can
    /// never enqueue an operation twice.
    private func executeWithLegacyFallback(
        _ command: String,
        legacyCommand: String,
        unsupportedFlags: [String]
    ) async throws -> String {
        do {
            return try await ssh.executeCommand(command)
        } catch {
            let message = error.localizedDescription
            let isUnsupported = unsupportedFlags.contains {
                message.contains("unknown flag: \($0)")
            }
            guard isUnsupported else { throw error }
            return try await ssh.executeCommand(legacyCommand)
        }
    }

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
                throw QueueError.invalidResponse("レスポンスの解析に失敗しました: \(error.localizedDescription)\nサーバー応答: \(rawString.truncatedForDisplay)")
            }
            throw QueueError.invalidResponse("レスポンスの解析に失敗しました: \(error.localizedDescription)")
        }
    }
}

private extension String {
    var truncatedForDisplay: String {
        let limit = 4_096
        guard count > limit else { return self }
        return String(prefix(limit)) + "\n…(truncated)"
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

    var errorDescription: String? {
        switch self {
        case .invalidResponse(let msg):
            return msg
        }
    }
}
