import Foundation

/// SSH connection manager using Process-based SSH execution.
class SSHConnection: ObservableObject, @unchecked Sendable {
    @Published var isConnected = false
    @Published var connectionError: String?

    let server: Server

    private let activeProcessesLock = NSLock()
    private var activeProcesses: [Process] = []
    private var invalidated = false

    init(server: Server) {
        self.server = server
    }

    func terminateAll() {
        activeProcessesLock.lock()
        invalidated = true
        let processes = activeProcesses
        activeProcesses.removeAll()
        activeProcessesLock.unlock()

        for process in processes where process.isRunning {
            process.terminate()
        }
    }

    func executeCommand(_ command: String, timeout: TimeInterval = 120) async throws -> String {
        try await withThrowingTaskGroup(of: String.self) { group in
            group.addTask {
                try await self.runCommand(command)
            }
            group.addTask {
                let boundedTimeout = timeout.isFinite ? min(max(timeout, 1), 86_400) : 120
                try await Task.sleep(nanoseconds: UInt64(boundedTimeout * 1_000_000_000))
                throw SSHError.commandTimedOut
            }

            guard let result = try await group.next() else {
                throw SSHError.invalidResponse
            }
            group.cancelAll()
            return result
        }
    }

    private func register(_ process: Process) throws {
        activeProcessesLock.lock()
        defer { activeProcessesLock.unlock() }
        if invalidated {
            throw CancellationError()
        }
        activeProcesses.append(process)
    }

    private func unregister(_ process: Process) {
        activeProcessesLock.lock()
        activeProcesses.removeAll { $0 === process }
        activeProcessesLock.unlock()
    }

    private func isInvalidated() -> Bool {
        activeProcessesLock.lock()
        defer { activeProcessesLock.unlock() }
        return invalidated
    }

    /// Reads a handle to EOF on the calling thread. Exactly one reader per pipe
    /// keeps the bytes in order, and the loop keeps draining past the size limit
    /// so the child never blocks writing into a full pipe.
    private func drain(_ handle: FileHandle, into sink: SendableData) {
        while true {
            let chunk = handle.availableData
            if chunk.isEmpty {
                break
            }
            sink.append(chunk)
        }
    }

    private func runCommand(_ command: String) async throws -> String {
        try Task.checkCancellation()

        let process = Process()
        try register(process)
        defer { unregister(process) }

        // onCancel can land between the dispatch below and process.run(), when
        // there is no running process for it to terminate; this lets the
        // launching thread notice and skip the spawn entirely.
        let cancelled = CancellationFlag()

        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                let pipe = Pipe()
                let errorPipe = Pipe()

                process.executableURL = URL(fileURLWithPath: "/usr/bin/ssh")
                process.arguments = buildSSHArguments(command: command)
                process.standardOutput = pipe
                process.standardError = errorPipe

                let outputData = SendableData(limit: 64 * 1024 * 1024)
                let errorData = SendableData(limit: 1024 * 1024)

                // Each pipe is drained to EOF by a single thread, then the exit
                // status is collected. The previous shape mixed a
                // readabilityHandler with a readDataToEndOfFile issued from the
                // termination handler: both read the same descriptor
                // concurrently, so chunks could be appended out of order and a
                // large listing came back as unparseable JSON.
                DispatchQueue.global(qos: .userInitiated).async { [self] in
                    guard !isInvalidated(), !cancelled.isSet else {
                        continuation.resume(throwing: CancellationError())
                        return
                    }
                    do {
                        try process.run()
                    } catch {
                        continuation.resume(throwing: SSHError.connectionFailed(error.localizedDescription))
                        return
                    }
                    if (cancelled.isSet || isInvalidated()) && process.isRunning {
                        process.terminate()
                    }

                    let stderrDrained = DispatchSemaphore(value: 0)
                    DispatchQueue.global(qos: .userInitiated).async { [self] in
                        drain(errorPipe.fileHandleForReading, into: errorData)
                        stderrDrained.signal()
                    }
                    drain(pipe.fileHandleForReading, into: outputData)
                    stderrDrained.wait()
                    process.waitUntilExit()
                    if cancelled.isSet || isInvalidated() {
                        continuation.resume(throwing: CancellationError())
                        return
                    }

                    let (data, outputExceeded) = outputData.get()
                    let (errorBytes, errorExceeded) = errorData.get()
                    // stderr is only used below, on the failure path, so an
                    // oversized stderr must not discard stdout - the actual
                    // payload - on an otherwise-successful command. stdout is
                    // still held to its own limit unconditionally.
                    if outputExceeded {
                        continuation.resume(throwing: SSHError.outputTooLarge)
                        return
                    }

                    if process.terminationStatus == 0 {
                        guard let output = String(data: data, encoding: .utf8) else {
                            continuation.resume(throwing: SSHError.invalidResponse)
                            return
                        }
                        continuation.resume(returning: output)
                        return
                    }

                    var message = String(data: errorBytes, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                    if message.isEmpty {
                        message = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "Unknown error (Exit code: \(process.terminationStatus))"
                    } else if errorExceeded {
                        message += "\n…(truncated)"
                    }

                    if message.contains("Permission denied") {
                        message = "認証に失敗しました (Permission denied)"
                    } else if message.contains("Connection timed out") || message.contains("Operation timed out") {
                        message = "接続がタイムアウトしました"
                    } else if message.contains("No route to host") {
                        message = "ホストに到達できません"
                    } else if message.contains("command not found") || message.contains("No such file or directory") && message.contains("file-exploder") {
                        message = "サーバーに file-exploder がインストールされていないか、PATHが通っていません。\n詳細: \(message)"
                    }
                    continuation.resume(throwing: SSHError.commandFailed(message))
                }
            }
        } onCancel: {
            cancelled.set()
            if process.isRunning {
                process.terminate()
            }
        }
    }

    func executeJSONCommand<T: Decodable>(_ command: String) async throws -> T {
        let output = try await executeCommand(command)
        guard let data = output.data(using: .utf8) else {
            throw SSHError.invalidResponse
        }
        let decoder = JSONDecoder.fileExploderDecoder()
        return try decoder.decode(T.self, from: data)
    }

    func testConnection() async throws {
        let output = try await executeCommand("echo 'connection_ok'")
        guard output.trimmingCharacters(in: .whitespacesAndNewlines).hasSuffix("connection_ok") else {
            throw SSHError.connectionFailed("Unexpected response: \(output)")
        }
        await MainActor.run {
            isConnected = true
            connectionError = nil
        }
    }

    private func buildSSHArguments(command: String) -> [String] {
        var arguments: [String] = [
            "-p", String(server.port),
            "-o", "StrictHostKeyChecking=accept-new",
            "-o", "ConnectTimeout=10",
            "-o", "BatchMode=yes",
            "-o", "ServerAliveInterval=5",
            "-o", "ServerAliveCountMax=3",
            "-o", "LogLevel=ERROR",
        ]

        if let keyPath = server.keyPath, !keyPath.isEmpty, server.authType == .sshKey {
            arguments.append(contentsOf: ["-i", keyPath])
        }
        arguments.append("\(server.username)@\(server.hostname)")
        arguments.append(command)
        return arguments
    }
}

enum SSHError: LocalizedError {
    case connectionFailed(String)
    case commandFailed(String)
    case commandTimedOut
    case outputTooLarge
    case invalidResponse
    case keyNotFound(String)

    var errorDescription: String? {
        switch self {
        case .connectionFailed(let reason):
            return "Connection failed: \(reason)"
        case .commandFailed(let reason):
            return "Command failed: \(reason)"
        case .commandTimedOut:
            return "リモートコマンドがタイムアウトしました"
        case .outputTooLarge:
            return "リモートコマンドの出力が大きすぎます"
        case .invalidResponse:
            return "Invalid response from server"
        case .keyNotFound(let path):
            return "SSH key not found at: \(path)"
        }
    }
}

private final class CancellationFlag: @unchecked Sendable {
    private let lock = NSLock()
    private var value = false

    var isSet: Bool {
        lock.lock()
        defer { lock.unlock() }
        return value
    }

    func set() {
        lock.lock()
        value = true
        lock.unlock()
    }
}

final class SendableData: @unchecked Sendable {
    private var data = Data()
    private let lock = NSLock()
    private let limit: Int
    private var exceeded = false

    init(limit: Int) {
        self.limit = limit
    }

    func append(_ newData: Data) {
        lock.lock()
        let remaining = max(limit - data.count, 0)
        if newData.count > remaining {
            data.append(contentsOf: newData.prefix(remaining))
            exceeded = true
        } else {
            data.append(newData)
        }
        lock.unlock()
    }

    func get() -> (Data, Bool) {
        lock.lock()
        let result = data
        let wasExceeded = exceeded
        lock.unlock()
        return (result, wasExceeded)
    }
}
