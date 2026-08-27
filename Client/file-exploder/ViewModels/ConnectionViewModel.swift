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
    private var defaultsObserver: NSObjectProtocol?

    var activeServerID: UUID? {
        connectedServer?.id ?? connectingServerID
    }
    
    init() {
        loadServers()
        // Cmd+N opens a second window, and each one builds its own view model
        // over the same defaults key. Without this, a window would keep showing
        // the list exactly as it looked when it opened.
        defaultsObserver = NotificationCenter.default.addObserver(
            forName: UserDefaults.didChangeNotification,
            object: nil,
            queue: nil
        ) { [weak self] _ in
            Task { @MainActor [weak self] in
                self?.reloadIfChanged()
            }
        }
    }

    deinit {
        if let defaultsObserver {
            NotificationCenter.default.removeObserver(defaultsObserver)
        }
    }
    
    func loadServers() {
        guard let stored = decodeStoredServers() else { return }
        servers = stored
    }

    /// Returns the persisted list, or nil when it is present but unreadable -
    /// in which case the caller keeps whatever it already has rather than
    /// treating a decode failure as "the user has no servers".
    private func decodeStoredServers() -> [Server]? {
        guard let data = UserDefaults.standard.data(forKey: serversKey) else {
            return []
        }
        do {
            return try JSONDecoder().decode([Server].self, from: data)
        } catch {
            print("Failed to decode saved servers: \(error)")
            connectionError = "サーバー設定の読み込みに失敗しました: \(error.localizedDescription)"
            return nil
        }
    }

    private func reloadIfChanged() {
        guard let stored = decodeStoredServers(), stored != servers else { return }
        servers = stored
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

    /// Applies a change to the list as it is stored *right now*. Writing back a
    /// copy this window loaded earlier would drop whatever another window saved
    /// in between: add a server in one window, then one in a second, and the
    /// first disappears.
    private func mutateServers(_ mutate: (inout [Server]) -> Void) {
        var list = decodeStoredServers() ?? servers
        mutate(&list)
        servers = list
        saveServers()
    }
    
    func addServer(_ server: Server) {
        mutateServers { $0.append(server) }
    }
    
    func updateServer(_ server: Server) {
        mutateServers { list in
            if let index = list.firstIndex(where: { $0.id == server.id }) {
                list[index] = server
            }
        }
    }
    
    func deleteServer(_ server: Server) {
        mutateServers { $0.removeAll { $0.id == server.id } }
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
