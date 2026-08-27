import Foundation
import SwiftUI

@MainActor
class ConnectionViewModel: ObservableObject {
    @Published var servers: [Server] = []
    @Published var connectedServer: Server?
    @Published var isConnecting = false
    @Published var connectionError: String?
    
    private let serversKey = "saved_servers"
    private var connectionGeneration = 0
    private var connectingServerID: UUID?

    var activeServerID: UUID? {
        connectedServer?.id ?? connectingServerID
    }
    
    init() {
        loadServers()
    }
    
    func loadServers() {
        guard let data = UserDefaults.standard.data(forKey: serversKey) else {
            return
        }
        do {
            servers = try JSONDecoder().decode([Server].self, from: data)
        } catch {
            print("Failed to decode saved servers: \(error)")
            connectionError = "サーバー設定の読み込みに失敗しました: \(error.localizedDescription)"
        }
    }
    
    func saveServers() {
        do {
            let data = try JSONEncoder().encode(servers)
            UserDefaults.standard.set(data, forKey: serversKey)
        } catch {
            print("Failed to encode servers: \(error)")
            connectionError = "サーバー設定の保存に失敗しました: \(error.localizedDescription)"
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
        if connectedServer?.id == server.id || connectingServerID == server.id {
            connectionGeneration += 1
            connectingServerID = nil
            isConnecting = false
            connectedServer = nil
        }
    }
    
    func connect(to server: Server, fileListVM: FileListViewModel) async {
        connectionGeneration += 1
        let generation = connectionGeneration
        connectingServerID = server.id
        isConnecting = true
        connectedServer = nil
        connectionError = nil
        
        await fileListVM.connect(server: server)
        guard generation == connectionGeneration else { return }
        
        if fileListVM.errorMessage == nil {
            connectedServer = server
        } else {
            connectionError = fileListVM.errorMessage
        }
        
        connectingServerID = nil
        isConnecting = false
    }
    
    func disconnect() {
        connectionGeneration += 1
        connectingServerID = nil
        isConnecting = false
        connectedServer = nil
    }
}
