import SwiftUI

struct FileListView: View {
    @ObservedObject var viewModel: FileListViewModel
    @State private var selectedFiles: Set<String> = []
    @State private var sortOrder = [KeyPathComparator(\RemoteFile.name)]
    @State private var searchText = ""
    @State private var showNewFolderSheet = false
    @State private var newFolderName = ""
    @State private var renamingFile: RemoteFile?
    @State private var renameText = ""
    @State private var movingFile: RemoteFile?
    @State private var moveDestinationText = ""
    @State private var copyingFile: RemoteFile?
    
    @State private var showingDeleteConfirmation = false
    @State private var filesToDelete: [RemoteFile] = []
    
    var filteredFiles: [RemoteFile] {
        if searchText.isEmpty {
            return viewModel.files
        }
        return viewModel.files.filter { $0.name.localizedCaseInsensitiveContains(searchText) }
    }
    
    var body: some View {
        VStack(spacing: 0) {
            // Toolbar
            toolbar
            
            Divider()
            
            // Breadcrumb
            BreadcrumbView(path: viewModel.currentPath) { path in
                Task { await viewModel.navigateTo(path: path) }
            }
            .padding(.horizontal)
            .padding(.vertical, 6)
            
            Divider()
            
            // File table
            if viewModel.isLoading {
                Spacer()
                ProgressView("読み込み中...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                Spacer()
            } else if let error = viewModel.errorMessage {
                Spacer()
                VStack(spacing: 12) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.largeTitle)
                        .foregroundColor(.orange)
                    Text(error)
                        .foregroundColor(.secondary)
                    Button("再試行") {
                        Task { await viewModel.refresh() }
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if filteredFiles.isEmpty {
                Spacer()
                VStack(spacing: 12) {
                    Image(systemName: "folder")
                        .font(.largeTitle)
                        .foregroundColor(.secondary)
                    Text("ファイルがありません")
                        .foregroundColor(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                Table(filteredFiles.sorted(using: sortOrder), selection: $selectedFiles, sortOrder: $sortOrder) {
                    TableColumn("名前", value: \.name) { file in
                        HStack(spacing: 8) {
                            Image(systemName: file.isDirectory ? "folder.fill" : fileIcon(for: file))
                                .foregroundColor(file.isDirectory ? .accentColor : .secondary)
                            
                            Text(file.name)
                                .lineLimit(1)
                        }
                    }
                    .width(min: 200, ideal: 300)
                    
                    TableColumn("サイズ", value: \.size) { file in
                        Text(file.formattedSize)
                            .foregroundColor(.secondary)
                    }
                    .width(min: 80, ideal: 100)
                    
                    TableColumn("更新日時", value: \.modificationDate) { file in
                        Text(file.formattedDate)
                            .foregroundColor(.secondary)
                    }
                    .width(min: 120, ideal: 150)
                    
                    TableColumn("権限", value: \.permissions.symbolicString) { file in
                        Text(file.permissions.symbolicString)
                            .font(.system(.body, design: .monospaced))
                            .foregroundColor(.secondary)
                    }
                    .width(min: 80, ideal: 100)
                }
                .contextMenu(forSelectionType: String.self) { selection in
                    if let fileId = selection.first,
                       let file = filteredFiles.first(where: { $0.id == fileId }) {
                        fileContextMenu(file: file)
                    }
                } primaryAction: { selection in
                    if let fileId = selection.first,
                       let file = filteredFiles.first(where: { $0.id == fileId }) {
                        Task { await viewModel.openFile(file) }
                    }
                }
            }
        }
        .searchable(text: $searchText, prompt: "ファイルを検索")
        .sheet(isPresented: $showNewFolderSheet) {
            NewFolderSheet(name: $newFolderName) {
                Task {
                    await viewModel.createFolder(name: newFolderName)
                    newFolderName = ""
                    showNewFolderSheet = false
                }
            }
        }
        .sheet(item: $renamingFile) { file in
            RenameSheet(name: $renameText) {
                Task {
                    await viewModel.renameFile(file, to: renameText)
                    renameText = ""
                    renamingFile = nil
                }
            }
        }
        .sheet(item: $movingFile) { file in
            MoveSheet(destination: $moveDestinationText) {
                Task {
                    await viewModel.moveFiles([file], to: moveDestinationText)
                    moveDestinationText = ""
                    movingFile = nil
                }
            }
        }
        .sheet(item: $copyingFile) { file in
            MoveSheet(destination: $moveDestinationText, isCopy: true) {
                Task {
                    await viewModel.copyFiles([file], to: moveDestinationText)
                    moveDestinationText = ""
                    copyingFile = nil
                }
            }
        }
        .confirmationDialog(
            "本当に削除しますか？",
            isPresented: $showingDeleteConfirmation,
            titleVisibility: .visible
        ) {
            Button("削除", role: .destructive) {
                let toDelete = filesToDelete
                Task {
                    await viewModel.deleteFiles(toDelete)
                    filesToDelete = []
                }
            }
            Button("キャンセル", role: .cancel) {
                filesToDelete = []
            }
        } message: {
            if filesToDelete.count == 1 {
                Text("\(filesToDelete[0].name) を完全に削除します。元に戻せません。")
            } else {
                Text("\(filesToDelete.count) 項目を完全に削除します。元に戻せません。")
            }
        }
    }
    
    // MARK: - Toolbar
    
    private var toolbar: some View {
        HStack(spacing: 12) {
            // Navigation buttons
            HStack(spacing: 4) {
                Button(action: { Task { await viewModel.goBack() } }) {
                    Image(systemName: "chevron.left")
                }
                .disabled(!viewModel.canGoBack)
                
                Button(action: { Task { await viewModel.goForward() } }) {
                    Image(systemName: "chevron.right")
                }
                .disabled(!viewModel.canGoForward)
                
                Button(action: { Task { await viewModel.goToParent() } }) {
                    Image(systemName: "chevron.up")
                }
            }
            .buttonStyle(.borderless)
            
            Divider()
                .frame(height: 20)
            
            // Actions
            Button(action: { showNewFolderSheet = true }) {
                Image(systemName: "folder.badge.plus")
            }
            .buttonStyle(.borderless)
            .help("新規フォルダ")
            
            Button(action: { Task { await viewModel.refresh() } }) {
                Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)
            .help("更新")
            
            Spacer()
            
            // Selection info
            if !selectedFiles.isEmpty {
                Text("\(selectedFiles.count) 項目を選択中")
                    .foregroundColor(.secondary)
                    .font(.caption)
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }
    
    // MARK: - Context Menu
    
    @ViewBuilder
    private func fileContextMenu(file: RemoteFile) -> some View {
        Group {
            if file.isDirectory {
                Button("開く") {
                    Task { await viewModel.openFile(file) }
                }
            }
            
            Divider()
            
            Button("名前の変更") {
                renameText = file.name
                renamingFile = file
            }
            
            Button("移動") {
                moveDestinationText = viewModel.currentPath
                movingFile = file
            }
            
            Button("サーバー内で複製") {
                let toCopy = filteredFiles.filter { selectedFiles.contains($0.id) }
                if toCopy.isEmpty, let file = filteredFiles.first(where: { $0.id == file.id }) {
                    moveDestinationText = viewModel.currentPath
                    copyingFile = file
                } else if !toCopy.isEmpty {
                    if let file = toCopy.first {
                        moveDestinationText = viewModel.currentPath
                        copyingFile = file
                    }
                }
            }
            
            Button("パスをコピー") {
                let toCopy = filteredFiles.filter { selectedFiles.contains($0.id) }
                if toCopy.isEmpty, let file = filteredFiles.first(where: { $0.id == file.id }) {
                    let pb = NSPasteboard.general
                    pb.clearContents()
                    pb.setString(file.path, forType: .string)
                } else if !toCopy.isEmpty {
                    let pb = NSPasteboard.general
                    pb.clearContents()
                    let paths = toCopy.map { $0.path }
                    pb.writeObjects(paths as [NSString])
                }
            }
            
            Button("パスをコピー") {
                let toCopy = filteredFiles.filter { selectedFiles.contains($0.id) }
                if toCopy.isEmpty, let file = filteredFiles.first(where: { $0.id == file.id }) {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(file.path, forType: .string)
                } else if !toCopy.isEmpty {
                    NSPasteboard.general.clearContents()
                    let paths = toCopy.map { $0.path }
                    NSPasteboard.general.writeObjects(paths as [NSString])
                }
            }
            
            Divider()
            
            Menu("権限") {
                ForEach(["755", "644", "777", "600"], id: \.self) { mode in
                    Button(mode) {
                        Task { await viewModel.changePermissions(file, mode: mode) }
                    }
                }
            }
            
            Divider()
            
            Button("削除", role: .destructive) {
                filesToDelete = [file]
                showingDeleteConfirmation = true
            }
        }
    }
    
    private func fileIcon(for file: RemoteFile) -> String {
        let ext = (file.name as NSString).pathExtension.lowercased()
        switch ext {
        case "jpg", "jpeg", "png", "gif", "svg", "webp":
            return "photo"
        case "pdf":
            return "doc.richtext"
        case "zip", "tar", "gz", "7z", "rar":
            return "archivebox"
        case "mp3", "wav", "aac", "flac":
            return "music.note"
        case "mp4", "mov", "avi", "mkv":
            return "film"
        case "txt", "md", "log":
            return "doc.text"
        case "json", "xml", "plist":
            return "doc.plaintext"
        case "sh", "bash", "zsh":
            return "terminal"
        case "py", "rb", "js", "ts", "swift":
            return "chevron.left.forwardslash.chevron.right"
        default:
            return "doc"
        }
    }
}

// MARK: - Helper views

struct NewFolderSheet: View {
    @Binding var name: String
    let onCreate: () -> Void
    @Environment(\.dismiss) private var dismiss
    
    var body: some View {
        VStack(spacing: 16) {
            Text("新規フォルダ")
                .font(.headline)
            
            TextField("フォルダ名", text: $name)
                .textFieldStyle(.roundedBorder)
                .onSubmit { onCreate() }
            
            HStack {
                Spacer()
                Button("キャンセル") {
                    name = ""
                    dismiss()
                }
                .keyboardShortcut(.cancelAction)
                
                Button("作成") {
                    onCreate()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(name.isEmpty)
            }
        }
        .padding()
        .frame(width: 300)
    }
}

struct RenameSheet: View {
    @Binding var name: String
    let onRename: () -> Void
    @Environment(\.dismiss) private var dismiss
    
    var body: some View {
        VStack(spacing: 16) {
            Text("名前の変更")
                .font(.headline)
            
            TextField("新しい名前", text: $name)
                .textFieldStyle(.roundedBorder)
                .onSubmit { onRename() }
            
            HStack {
                Spacer()
                Button("キャンセル") {
                    name = ""
                    dismiss()
                }
                .keyboardShortcut(.cancelAction)
                
                Button("変更") {
                    onRename()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(name.isEmpty)
            }
        }
        .padding()
        .frame(width: 300)
    }
}

struct MoveSheet: View {
    @Binding var destination: String
    var isCopy: Bool = false
    let onMove: () -> Void
    @Environment(\.dismiss) private var dismiss
    
    var body: some View {
        VStack(spacing: 16) {
            Text(isCopy ? "サーバー内で複製" : "移動")
                .font(.headline)
            
            TextField(isCopy ? "複製先のディレクトリ" : "移動先のディレクトリ", text: $destination)
                .textFieldStyle(.roundedBorder)
                .onSubmit { onMove() }
            
            HStack {
                Spacer()
                Button("キャンセル") {
                    destination = ""
                    dismiss()
                }
                .keyboardShortcut(.cancelAction)
                
                Button(isCopy ? "複製" : "移動") {
                    onMove()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(destination.isEmpty)
            }
        }
        .padding()
        .frame(width: 300)
    }
}

#Preview {
    FileListView(viewModel: FileListViewModel())
}
