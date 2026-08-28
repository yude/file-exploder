import SwiftUI
import UniformTypeIdentifiers

struct ConnectionSheet: View {
    @ObservedObject var connectionVM: ConnectionViewModel
    @ObservedObject var fileListVM: FileListViewModel
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
    var validPort: UInt16? {
        guard let value = UInt16(port), value > 0 else { return nil }
        return value
    }
    var trimmedRoot: String {
        remoteRoot.trimmingCharacters(in: .whitespacesAndNewlines)
    }
    var keyPathIsValid: Bool {
        let trimmed = keyPath.trimmingCharacters(in: .whitespacesAndNewlines)
        return !trimmed.contains("\0") && (trimmed.isEmpty || trimmed.hasPrefix("/"))
    }
    var connectionFieldsAreValid: Bool {
        let trimmedHost = hostname.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedUser = username.trimmingCharacters(in: .whitespacesAndNewlines)
        return !trimmedHost.isEmpty &&
            !trimmedHost.contains(where: { $0.isWhitespace || $0.isNewline || $0 == "\0" }) &&
            !trimmedUser.isEmpty &&
            !trimmedUser.contains(where: { $0.isWhitespace || $0.isNewline || $0 == "\0" || $0 == "@" })
    }
    
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
                            Text(type.displayName).tag(type)
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
                .disabled(
                    name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ||
                    !connectionFieldsAreValid ||
                    validPort == nil ||
                    !trimmedRoot.hasPrefix("/") || trimmedRoot.contains("\0") ||
                    (authType == .sshKey && !keyPathIsValid)
                )
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
        .onChange(of: port) {
            // Filter non-numeric characters for port
            let filtered = port.filter { "0123456789".contains($0) }
            if filtered != port {
                port = filtered
            }
        }
        .fileImporter(
            isPresented: $showKeyPicker,
            allowedContentTypes: [.item],
            allowsMultipleSelection: false
        ) { result in
            if case .success(let urls) = result, let url = urls.first {
                keyPath = url.path
            }
        }
    }
    
    private func save() {
        if let portInt = validPort {
            // The root lives on the server; the key path is local, so only the
            // latter may be standardized against this machine's filesystem.
            let normalizedRoot = RemotePath.standardized(trimmedRoot)
            let trimmedKeyPath = keyPath.trimmingCharacters(in: .whitespacesAndNewlines)
            let normalizedKeyPath = trimmedKeyPath.isEmpty ? nil : (trimmedKeyPath as NSString).standardizingPath
            let newServer = Server(
                name: name.trimmingCharacters(in: .whitespacesAndNewlines),
                hostname: hostname.trimmingCharacters(in: .whitespacesAndNewlines),
                port: portInt,
                username: username.trimmingCharacters(in: .whitespacesAndNewlines),
                authType: authType,
                keyPath: authType == .sshKey ? normalizedKeyPath : nil,
                remoteRoot: normalizedRoot
            )
            
            if let existing = server {
                if connectionVM.activeServerID == existing.id {
                    fileListVM.disconnect()
                    connectionVM.disconnect()
                }
                var updated = newServer
                updated.id = existing.id
                connectionVM.updateServer(updated)
            } else {
                connectionVM.addServer(newServer)
            }
            
            dismiss()
        }
    }
}

#Preview {
    ConnectionSheet(
        connectionVM: ConnectionViewModel(),
        fileListVM: FileListViewModel(),
        server: nil
    )
}
