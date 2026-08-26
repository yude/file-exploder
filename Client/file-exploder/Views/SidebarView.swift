import SwiftUI

struct SidebarView: View {
    @ObservedObject var connectionVM: ConnectionViewModel
    @ObservedObject var fileListVM: FileListViewModel
    @State private var showAddServer = false
    @State private var editingServer: Server?
    
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                Text("サーバー")
                    .font(.headline)
                Spacer()
                Button(action: { showAddServer = true }) {
                    Image(systemName: "plus")
                }
                .buttonStyle(.borderless)
            }
            .padding(.horizontal)
            .padding(.vertical, 8)
            
            Divider()
            
            // Server list
            List {
                ForEach(connectionVM.servers) { server in
                    ServerRow(
                        server: server,
                        isConnected: connectionVM.connectedServer?.id == server.id,
                        onSelect: {
                            Task {
                                await connectionVM.connect(to: server, fileListVM: fileListVM)
                            }
                        },
                        onEdit: {
                            editingServer = server
                        },
                        onDelete: {
                            connectionVM.deleteServer(server)
                        }
                    )
                }
            }
            .listStyle(.sidebar)
            
            // Connection status
            if connectionVM.isConnecting {
                HStack {
                    ProgressView()
                        .controlSize(.small)
                    Text("接続中...")
                        .foregroundColor(.secondary)
                }
                .padding(.horizontal)
                .padding(.vertical, 8)
            } else if let error = connectionVM.connectionError {
                HStack {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundColor(.red)
                    Text(error)
                        .foregroundColor(.red)
                        .font(.caption)
                }
                .padding(.horizontal)
                .padding(.vertical, 8)
            }
        }
        .sheet(isPresented: $showAddServer) {
            ConnectionSheet(connectionVM: connectionVM, server: nil)
        }
        .sheet(item: $editingServer) { server in
            ConnectionSheet(connectionVM: connectionVM, server: server)
        }
    }
}

struct ServerRow: View {
    let server: Server
    let isConnected: Bool
    let onSelect: () -> Void
    let onEdit: () -> Void
    let onDelete: () -> Void
    
    var body: some View {
        Button(action: onSelect) {
            HStack {
                Image(systemName: isConnected ? "externaldrive.badge.checkmark" : "externaldrive")
                    .foregroundColor(isConnected ? .green : .secondary)
                
                VStack(alignment: .leading, spacing: 2) {
                    Text(server.name)
                        .foregroundColor(.primary)
                    Text("\(server.username)@\(server.hostname)")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                
                Spacer()
                
                if isConnected {
                    Circle()
                        .fill(.green)
                        .frame(width: 8, height: 8)
                }
            }
        }
        .buttonStyle(.plain)
        .contextMenu {
            Button("編集") { onEdit() }
            Divider()
            Button("削除", role: .destructive) { onDelete() }
        }
    }
}

#Preview {
    SidebarView(
        connectionVM: ConnectionViewModel(),
        fileListVM: FileListViewModel()
    )
}
