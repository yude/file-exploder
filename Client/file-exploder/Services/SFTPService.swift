import Foundation

/// SFTP service for directory listing and file operations
class SFTPService {
    private let ssh: SSHConnection
    private let commandPrefix = "PATH=\"$PATH:/usr/local/bin:$HOME/.local/bin\"; export PATH; file-exploder"
    
    init(ssh: SSHConnection) {
        self.ssh = ssh
    }
    
    /// A single `list` invocation enumerates and lstats every entry of a
    /// directory in one unpaginated pass (see Server/cmd/list.go), so it can
    /// legitimately take much longer than an ordinary control command on a
    /// very large directory or a slow filesystem. The generic 120s default
    /// would time it out - permanently, since retrying hits the same
    /// directory-size-driven slowness every time - so give it its own,
    /// much larger budget instead of inheriting executeCommand's default.
    private static let listDirectoryTimeout: TimeInterval = 900

    /// A directory large enough to need listDirectoryTimeout's longer budget
    /// is also large enough to produce a JSON response well past
    /// SSHConnection's default 64MB stdout cap - which would otherwise fail
    /// exactly that case with outputTooLarge instead of succeeding within the
    /// longer timeout. 512MB comfortably covers directories far larger than
    /// the 900s timeout is itself sized for.
    private static let listDirectoryOutputLimit = 512 * 1024 * 1024

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
            unsupportedFlags: ["--path-base64"],
            timeout: Self.listDirectoryTimeout,
            outputLimit: Self.listDirectoryOutputLimit
        )
        guard let data = jsonPayload(of: output) else {
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
        guard let data = jsonPayload(of: output),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = json["id"] as? String else {
            throw QueueError.invalidResponse("不正なジョブ登録レスポンス: \(output.truncatedForDisplay)")
        }
        return id
    }
    
    /// Get queue status
    func getQueueStatus() async throws -> [QueueJob] {
        let output = try await ssh.executeCommand("\(commandPrefix) status")
        guard let data = jsonPayload(of: output) else {
            throw QueueError.invalidResponse("Empty response")
        }
        let response = try parseJSON(data, type: QueueResponse.self)
        return (response.jobs ?? []).compactMap(\.value)
    }

    /// Get recent job logs
    ///
    /// Decoded element-by-element (see FailableDecodable) so one job with an
    /// operation type or status this client doesn't recognize - a newer
    /// server, or a build skew between client and server - doesn't hide every
    /// other job in the response.
    func getJobLogs(limit: Int = 50) async throws -> [QueueJob] {
        let output = try await ssh.executeCommand("\(commandPrefix) log --limit \(limit)")
        guard let data = jsonPayload(of: output) else {
            throw QueueError.invalidResponse("Empty response")
        }
        return try parseJSON(data, type: [FailableDecodable<QueueJob>].self).compactMap(\.value)
    }
    
    /// Get specific job status
    func getJobStatus(id: String) async throws -> QueueJob {
        let output = try await ssh.executeCommand("\(commandPrefix) status -- \(id.shellEscaped)")
        guard let data = jsonPayload(of: output) else {
            throw QueueError.invalidResponse("Empty response")
        }
        return try parseJSON(data, type: QueueJob.self)
    }
    
    /// How long a job may sit pending, with nothing else running, before the
    /// queue is treated as stalled.
    private static let stalledQueueGracePeriod: TimeInterval = 30

    /// Consecutive polling failures tolerated before a wait gives up. Each poll
    /// is its own SSH connection, so one refused connection or dropped session
    /// used to abort the wait and report a queued operation as failed - while
    /// the server went on to run it. Only an unbroken run of failures means the
    /// connection is really gone.
    private static let pollFailuresTolerated = 3

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

        var consecutiveFailures = 0

        while timeout == nil || Date().timeIntervalSince(start) < timeout! {
            try Task.checkCancellation()

            let job: QueueJob
            do {
                job = try await getJobStatus(id: id)
            } catch is CancellationError {
                throw CancellationError()
            } catch {
                consecutiveFailures += 1
                if consecutiveFailures > Self.pollFailuresTolerated {
                    throw error
                }
                try await Task.sleep(nanoseconds: pollInterval)
                pollInterval = min(pollInterval * 2, maxPollInterval)
                continue
            }
            consecutiveFailures = 0

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
                    // A failure here says nothing about the queue, so treat it
                    // as "still moving" and let the next poll decide.
                    if (try? await queueIsMoving()) ?? true {
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

    /// Concurrency cap for resolving several jobs' final outcomes at once:
    /// high enough that a burst of simultaneous completions in a large batch
    /// doesn't serialize behind each other's SSH round trip, low enough that
    /// a very large batch doesn't spawn an unbounded number of ssh child
    /// processes in one instant.
    private static let maxConcurrentJobLookups = 8

    /// Waits for every job in `ids` to leave the active queue, sharing a
    /// single getQueueStatus() poll per interval across the whole batch
    /// instead of paying one getJobStatus() SSH round trip per job per poll -
    /// which is what a bulk operation (multi-file delete/copy/move/chmod)
    /// used to do by calling waitForJob(id:) once per file, fully serially.
    ///
    /// Once a job leaves the active list it is looked up individually exactly
    /// once, to learn how it actually finished - a bounded number of these
    /// lookups run concurrently, so a burst of simultaneous completions
    /// doesn't serialize behind each other's SSH round trip either. Everything
    /// else about the wait - the stalled-queue detection, the poll-failure
    /// tolerance - mirrors waitForJob(id:) above, just shared across the
    /// batch instead of tracked per job. The one exception is deliberate:
    /// whether a job has ever been seen running is tracked per id (not one
    /// shared flag), so one job starting quickly doesn't disable stalled-queue
    /// detection for every other id in the same batch - a job that hasn't
    /// itself started waits out the same grace period waitForJob(id:) would
    /// give it alone.
    ///
    /// Returns the ids that did not complete successfully, mapped to an error
    /// message; an id that isn't a key in the result succeeded.
    func waitForJobs(ids: [String]) async throws -> [String: String] {
        guard !ids.isEmpty else { return [:] }

        var remaining = Set(ids)
        var failures: [String: String] = [:]
        var startedIDs: Set<String> = []

        var pollInterval: UInt64 = 300_000_000
        let maxPollInterval: UInt64 = 3_000_000_000
        var pendingSince = Date()
        var stalledObservations = 0
        var consecutiveFailures = 0

        while !remaining.isEmpty {
            try Task.checkCancellation()

            let activeJobs: [QueueJob]
            do {
                activeJobs = try await getQueueStatus()
            } catch is CancellationError {
                throw CancellationError()
            } catch {
                consecutiveFailures += 1
                if consecutiveFailures > Self.pollFailuresTolerated {
                    throw error
                }
                try await Task.sleep(nanoseconds: pollInterval)
                pollInterval = min(pollInterval * 2, maxPollInterval)
                continue
            }
            consecutiveFailures = 0

            let activeByID = Dictionary(uniqueKeysWithValues: activeJobs.map { ($0.id, $0) })
            let queueIsMoving = activeJobs.contains { $0.status == .running }
            for id in remaining where activeByID[id]?.status == .running {
                startedIDs.insert(id)
            }

            let justFinished = remaining.filter { activeByID[$0] == nil }
            for (id, result) in await Self.resolveOutcomes(justFinished, using: getJobStatus) {
                remaining.remove(id)
                switch result {
                case .success(let job):
                    switch job.status {
                    case .completed:
                        break
                    case .failed:
                        failures[id] = job.error ?? "Unknown error"
                    case .cancelled:
                        failures[id] = "ジョブがキャンセルされました"
                    case .pending, .running:
                        // Reappeared between the batch poll and this lookup -
                        // still moving; pick it back up next round.
                        remaining.insert(id)
                    }
                case .failure(let error):
                    failures[id] = error.localizedDescription
                }
            }
            if remaining.isEmpty { break }

            // Only ids that have never themselves started are eligible for
            // the stalled-queue failure below; an id that already started
            // keeps waiting unbounded, exactly like waitForJob(id:) does for
            // a single job.
            let neverStarted = remaining.subtracting(startedIDs)
            if !neverStarted.isEmpty, Date().timeIntervalSince(pendingSince) > Self.stalledQueueGracePeriod {
                if queueIsMoving {
                    pendingSince = Date()
                    stalledObservations = 0
                } else {
                    stalledObservations += 1
                    if stalledObservations >= 2 {
                        let message = "ジョブが開始されないままです。サーバーで file-exploder デーモンが動作しているか確認してください "
                            + "(systemctl --user status file-exploder)。ジョブはキューに残っています。"
                        for id in neverStarted {
                            failures[id] = message
                        }
                        remaining.subtract(neverStarted)
                        if remaining.isEmpty { break }
                    }
                }
            }

            try await Task.sleep(nanoseconds: pollInterval)
            pollInterval = min(pollInterval * 2, maxPollInterval)
        }

        return failures
    }

    /// Resolves each id in `ids` with `lookup`, running up to
    /// maxConcurrentJobLookups at once instead of one at a time, so a burst of
    /// simultaneously-finished jobs doesn't serialize behind each other's SSH
    /// round trip.
    private static func resolveOutcomes(
        _ ids: some Sequence<String>,
        using lookup: @escaping (String) async throws -> QueueJob
    ) async -> [(id: String, result: Result<QueueJob, Error>)] {
        await withTaskGroup(of: (String, Result<QueueJob, Error>).self) { group in
            var iterator = ids.makeIterator()

            func launchNext() {
                guard let id = iterator.next() else { return }
                group.addTask {
                    do {
                        return (id, .success(try await lookup(id)))
                    } catch {
                        return (id, .failure(error))
                    }
                }
            }
            for _ in 0..<maxConcurrentJobLookups { launchNext() }

            var collected: [(String, Result<QueueJob, Error>)] = []
            while let outcome = await group.next() {
                collected.append(outcome)
                launchNext()
            }
            return collected
        }
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
        unsupportedFlags: [String],
        timeout: TimeInterval = 120,
        outputLimit: Int = SSHConnection.defaultOutputLimit
    ) async throws -> String {
        do {
            return try await ssh.executeCommand(command, timeout: timeout, outputLimit: outputLimit)
        } catch {
            let message = error.localizedDescription
            let isUnsupported = unsupportedFlags.contains {
                message.contains("unknown flag: \($0)")
            }
            guard isUnsupported else { throw error }
            return try await ssh.executeCommand(legacyCommand, timeout: timeout, outputLimit: outputLimit)
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
    
    /// The JSON a server command produced, picked out of whatever else came
    /// back on stdout.
    ///
    /// Every command writes exactly one JSON document on exactly one line —
    /// encoding/json's Encoder emits compact output followed by a newline — but
    /// it shares stdout with the login shell that ran it. A banner, a fortune,
    /// an `echo` in a profile above the non-interactive guard: any of it arrives
    /// first and used to make every single command fail to parse, with nothing
    /// in the app working until the server's shell startup was tracked down.
    /// A literal newline cannot appear inside a JSON string, so the last
    /// non-empty line is the document.
    private func jsonPayload(of output: String) -> Data? {
        let lastLine = output
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .last { !$0.isEmpty }
        return (lastLine ?? output).data(using: .utf8)
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

struct QueueResponse: Decodable {
    let total: Int
    let jobs: [FailableDecodable<QueueJob>]?
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
