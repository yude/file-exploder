import Foundation
import Network

/// SSH connection manager using Process-based SSH execution
/// For production, this would use libssh2 directly
class SSHConnection: ObservableObject, @unchecked Sendable {
    @Published var isConnected = false
    @Published var connectionError: String?
    
    let server: Server
    var process: Process? // Public access for disconnection
    
    private let activeProcessesLock = NSLock()
    private var activeProcesses: [Process] = []
    
    init(server: Server) {
        self.server = server
    }
    
    func terminateAll() {
        activeProcessesLock.lock()
        let procs = activeProcesses
        activeProcesses.removeAll()
        activeProcessesLock.unlock()
        
        for p in procs {
            if p.isRunning {
                p.terminate()
            }
        }
        if let p = process, p.isRunning {
            p.terminate()
        }
    }
    
    /// Execute a command over SSH and return the output
    func executeCommand(_ command: String) async throws -> String {
        let process = Process()
        
        activeProcessesLock.lock()
        activeProcesses.append(process)
        activeProcessesLock.unlock()
        
        defer {
            activeProcessesLock.lock()
            activeProcesses.removeAll { $0 === process }
            activeProcessesLock.unlock()
        }
        
        return try await withTaskCancellationHandler {
            return try await withCheckedThrowingContinuation { continuation in
                let pipe = Pipe()
                let errorPipe = Pipe()
                
                process.executableURL = URL(fileURLWithPath: "/usr/bin/ssh")
                process.arguments = buildSSHArguments(command: command)
                process.standardOutput = pipe
                process.standardError = errorPipe
                
                self.process = process // Store reference
                
                let outputData = SendableData()
                let errorData = SendableData()
                
                pipe.fileHandleForReading.readabilityHandler = { handle in
                    let data = handle.availableData
                    outputData.append(data)
                }
                
                errorPipe.fileHandleForReading.readabilityHandler = { handle in
                    let data = handle.availableData
                    errorData.append(data)
                }
                
                process.terminationHandler = { _ in
                    pipe.fileHandleForReading.readabilityHandler = nil
                    errorPipe.fileHandleForReading.readabilityHandler = nil
                    
                    outputData.append(pipe.fileHandleForReading.readDataToEndOfFile())
                    errorData.append(errorPipe.fileHandleForReading.readDataToEndOfFile())
                    
                    let data = outputData.get()
                    let err = errorData.get()
                
                    if process.terminationStatus == 0 {
                        let output = String(data: data, encoding: .utf8) ?? ""
                        continuation.resume(returning: output)
                    } else {
                        var errorStr = String(data: err, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                        if errorStr.isEmpty {
                            errorStr = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "Unknown error (Exit code: \(process.terminationStatus))"
                        }
                        
                        // 既知のSSHやコマンドエラーをユーザーフレンドリーに変換
                        if errorStr.contains("Permission denied") {
                            errorStr = "認証に失敗しました (Permission denied)"
                        } else if errorStr.contains("Connection timed out") || errorStr.contains("Operation timed out") {
                            errorStr = "接続がタイムアウトしました"
                        } else if errorStr.contains("No route to host") {
                            errorStr = "ホストに到達できません"
                        } else if errorStr.contains("command not found") || errorStr.contains("No such file or directory") && errorStr.contains("file-exploder") {
                            errorStr = "サーバーに file-exploder がインストールされていないか、PATHが通っていません。\n詳細: \(errorStr)"
                        }
                        
                        continuation.resume(throwing: SSHError.commandFailed(errorStr))
                    }
                }
                
                do {
                    try process.run()
                } catch {
                    continuation.resume(throwing: SSHError.connectionFailed(error.localizedDescription))
                }
            }
        } onCancel: {
            if process.isRunning {
                process.terminate()
            }
        }
    }
    
    /// Execute a command and return parsed JSON output
    func executeJSONCommand<T: Decodable>(_ command: String) async throws -> T {
        let output = try await executeCommand(command)
        guard let data = output.data(using: .utf8) else {
            throw SSHError.invalidResponse
        }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(T.self, from: data)
    }
    
    /// Test the SSH connection
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
        var args: [String] = []
        
        // Connection options
        args.append(contentsOf: ["-p", String(server.port)])
        // Disable known hosts checking for simplicity
        // Note: StrictHostKeyChecking=accept-new is better but not supported by all SSH versions
        args.append(contentsOf: ["-o", "StrictHostKeyChecking=accept-new"])
        args.append(contentsOf: ["-o", "ConnectTimeout=10"])
        args.append(contentsOf: ["-o", "ServerAliveInterval=5"])
        args.append(contentsOf: ["-o", "ServerAliveCountMax=3"])
        args.append(contentsOf: ["-o", "LogLevel=ERROR"])
        
        // Auth options
        if let keyPath = server.keyPath, server.authType == .sshKey {
            args.append(contentsOf: ["-i", keyPath])
        }
        
        // Target
        args.append("\(server.username)@\(server.hostname)")
        
        // Command
        args.append(command)
        
        return args
    }
}

enum SSHError: LocalizedError {
    case connectionFailed(String)
    case commandFailed(String)
    case invalidResponse
    case keyNotFound(String)
    
    var errorDescription: String? {
        switch self {
        case .connectionFailed(let reason):
            return "Connection failed: \(reason)"
        case .commandFailed(let reason):
            return "Command failed: \(reason)"
        case .invalidResponse:
            return "Invalid response from server"
        case .keyNotFound(let path):
            return "SSH key not found at: \(path)"
        }
    }
}

final class SendableData: @unchecked Sendable {
    private var data = Data()
    private let lock = NSLock()
    
    func append(_ newData: Data) {
        lock.lock()
        data.append(newData)
        lock.unlock()
    }
    
    func get() -> Data {
        lock.lock()
        let result = data
        lock.unlock()
        return result
    }
}
