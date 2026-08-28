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
        case sshKey

        /// Deliberately not the raw value. The raw value is what lands in the
        /// saved server list, so spelling the label there would mean any change
        /// to the wording - a translation, a clearer term, a second auth method
        /// named in the same style - silently made every stored server
        /// undecodable, and the list would come back empty.
        var displayName: String {
            switch self {
            case .sshKey:
                return "SSHキー"
            }
        }

        /// Also accepts the label this used to be stored as, so lists written by
        /// earlier versions still load.
        init(from decoder: Decoder) throws {
            let container = try decoder.singleValueContainer()
            let raw = try container.decode(String.self)
            switch raw {
            case AuthType.sshKey.rawValue, "SSHキー":
                self = .sshKey
            default:
                throw DecodingError.dataCorruptedError(
                    in: container,
                    debugDescription: "Unknown authentication type: \(raw)"
                )
            }
        }
    }
    
    static let defaultPort: UInt16 = 22
    
    init(name: String, hostname: String, port: UInt16 = Server.defaultPort, username: String, authType: AuthType = .sshKey, keyPath: String? = nil, remoteRoot: String = "/") {
        self.name = name
        self.hostname = hostname
        self.port = port
        self.username = username
        self.authType = authType
        self.keyPath = keyPath
        self.remoteRoot = remoteRoot
    }
}
