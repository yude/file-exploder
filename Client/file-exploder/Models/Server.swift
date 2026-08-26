import Foundation

struct Server: Identifiable, Codable, Hashable {
    var id = UUID()
    var name: String
    var hostname: String
    var port: UInt16
    var username: String
    var authType: AuthType
    var keyPath: String?
    var remoteRoot: String
    
    enum AuthType: String, Codable, CaseIterable {
        case password = "パスワード"
        case sshKey = "SSHキー"
    }
    
    static let defaultPort: UInt16 = 22
    
    init(name: String, hostname: String, port: UInt16 = 22, username: String, authType: AuthType = .sshKey, keyPath: String? = nil, remoteRoot: String = "/") {
        self.name = name
        self.hostname = hostname
        self.port = port
        self.username = username
        self.authType = authType
        self.keyPath = keyPath
        self.remoteRoot = remoteRoot
    }
}
