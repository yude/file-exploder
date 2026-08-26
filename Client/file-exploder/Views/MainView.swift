import SwiftUI

struct MainView: View {
    @StateObject private var connectionVM = ConnectionViewModel()
    @StateObject private var fileListVM = FileListViewModel()
    @State private var showQueuePanel = false
    
    var body: some View {
        HSplitView {
            // Sidebar
            SidebarView(connectionVM: connectionVM, fileListVM: fileListVM)
                .frame(minWidth: 200, idealWidth: 250, maxWidth: 300)
            
            // Main content
            VStack(spacing: 0) {
                // Title bar info
                if let server = connectionVM.connectedServer {
                    HStack {
                        Image(systemName: "externaldrive.fill")
                            .foregroundColor(.green)
                        Text(server.name)
                            .font(.subheadline)
                            .fontWeight(.medium)
                        Text("(\(server.username)@\(server.hostname))")
                            .font(.caption)
                            .foregroundColor(.secondary)
                        
                        Spacer()
                        
                        Button(action: {
                            fileListVM.disconnect()
                            connectionVM.disconnect()
                            showQueuePanel = false
                        }) {
                            Image(systemName: "xmark.circle")
                                .foregroundColor(.red)
                        }
                        .buttonStyle(.borderless)
                        .help("切断")
                        
                        Button(action: { showQueuePanel.toggle() }) {
                            Image(systemName: "list.bullet.rectangle")
                        }
                        .buttonStyle(.borderless)
                        .help("キューパネルを切り替え")
                    }
                    .padding(.horizontal)
                    .padding(.vertical, 6)
                    
                    Divider()
                }
                
                // File list
                FileListView(viewModel: fileListVM)
            }
            
            // Queue panel (optional)
            if showQueuePanel {
                QueuePanelView(sftp: fileListVM.sftp)
            }
        }
        .frame(minWidth: 800, minHeight: 500)
        .toolbar {
            ToolbarItemGroup(placement: .navigation) {
                Button(action: { showQueuePanel.toggle() }) {
                    Label("キュー", systemImage: "list.bullet.rectangle")
                }
            }
        }
    }
}

#Preview {
    MainView()
}
