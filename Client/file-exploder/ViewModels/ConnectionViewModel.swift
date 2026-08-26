import Foundation
import SwiftUI

@MainActor
class ConnectionViewModel: ObservableObject {
    @Published var servers: [Server] = []
    @Published var connectedServer: Server?
    @Published var isConnecting = false
    @Published var connectionError: String?
    
    private let serversKey = "saved_servers"
    
    init() {
        loadServers()
    }
    
    func loadServers() {
        guard let data = UserDefaults.standard.data(forKey: serversKey),
              let decoded = try? JSONDecoder().decode([Server].self, from: data) else {
            return
        }
        servers = decoded
    }
    
    func saveServers() {
        if let data = try? JSONEncoder().encode(servers) {
            UserDefaults.standard.set(data, forKey: serversKey)
        }
    }
    
    func addServer(_ server: Server) {
        servers.append(server)
        saveServers()
    }
    
    func updateServer(_ server: Server) {
        if let index = servers.firstIndex(where: { $0.id == server.id }) {
            servers[index] = server
            saveServers()
        }
    }
    
    func deleteServer(_ server: Server) {
        servers.removeAll { $0.id == server.id }
        saveServers()
        if connectedServer?.id == server.id {
            connectedServer = nil
        }
    }
    
    func connect(to server: Server, fileListVM: FileListViewModel) async {
        isConnecting = true
        connectionError = nil
        
        await fileListVM.connect(server: server)
        
        if fileListVM.errorMessage == nil {
            connectedServer = server
        } else {
            connectionError = fileListVM.errorMessage
        }
        
        isConnecting = false
    }
    
    func disconnect() {
        connectedServer = nil
    }
}
