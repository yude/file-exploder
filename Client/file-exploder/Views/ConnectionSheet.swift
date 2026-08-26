import SwiftUI

struct ConnectionSheet: View {
    @ObservedObject var connectionVM: ConnectionViewModel
    let server: Server?
    
    @State private var name = ""
    @State private var hostname = ""
    @State private var port = "22"
    @State private var username = ""
    @State private var authType: Server.AuthType = .sshKey
    @State private var keyPath = ""
    @State private var remoteRoot = "/"
    @State private var showKeyPicker = false
    
    @Environment(\.dismiss) private var dismiss
    
    var isEditing: Bool { server != nil }
    
    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text(isEditing ? "サーバーの編集" : "サーバーの追加")
                    .font(.headline)
                Spacer()
                Button(action: { dismiss() }) {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundColor(.secondary)
                }
                .buttonStyle(.borderless)
            }
            .padding()
            
            Divider()
            
            // Form
            Form {
                Section("接続設定") {
                    TextField("表示名", text: $name)
                    TextField("ホスト名", text: $hostname)
                    TextField("ポート番号", text: $port)
                    TextField("ユーザー名", text: $username)
                }
                
                Section("認証方法") {
                    Picker("認証タイプ", selection: $authType) {
                        ForEach(Server.AuthType.allCases, id: \.self) { type in
                            Text(type.rawValue).tag(type)
                        }
                    }
                    
                    if authType == .sshKey {
                        HStack {
                            TextField("SSHキーパス", text: $keyPath)
                            Button("参照") {
                                showKeyPicker = true
                            }
                        }
                    }
                }
                
                Section("リモート設定") {
                    TextField("ルートディレクトリ", text: $remoteRoot)
                }
            }
            .formStyle(.grouped)
            
            Divider()
            
            // Buttons
            HStack {
                Spacer()
                
                Button("キャンセル") {
                    dismiss()
                }
                .keyboardShortcut(.cancelAction)
                
                Button(isEditing ? "保存" : "追加") {
                    save()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(name.isEmpty || hostname.isEmpty || username.isEmpty)
            }
            .padding()
        }
        .frame(width: 450, height: 500)
        .onAppear {
            if let server = server {
                name = server.name
                hostname = server.hostname
                port = String(server.port)
                username = server.username
                authType = server.authType
                keyPath = server.keyPath ?? ""
                remoteRoot = server.remoteRoot
            }
        }
        .fileImporter(
            isPresented: $showKeyPicker,
            allowedContentTypes: [],
            allowsMultipleSelection: false
        ) { result in
            if case .success(let urls) = result, let url = urls.first {
                keyPath = url.path
            }
        }
    }
    
    private func save() {
        let newServer = Server(
            name: name,
            hostname: hostname,
            port: UInt16(port) ?? 22,
            username: username,
            authType: authType,
            keyPath: authType == .sshKey ? keyPath : nil,
            remoteRoot: remoteRoot
        )
        
        if let existing = server {
            var updated = newServer
            updated.id = existing.id
            connectionVM.updateServer(updated)
        } else {
            connectionVM.addServer(newServer)
        }
        
        dismiss()
    }
}

#Preview {
    ConnectionSheet(connectionVM: ConnectionViewModel(), server: nil)
}
