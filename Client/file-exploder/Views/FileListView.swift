import SwiftUI
#if os(macOS)
import AppKit
#endif

struct FileListView: View {
    @ObservedObject var viewModel: FileListViewModel
    @State private var selectedFiles: Set<String> = []
    @State private var sortOrder = [KeyPathComparator(\RemoteFile.name)]
    @State private var searchText = ""
    @State private var showNewFolderSheet = false
    @State private var newFolderName = ""
    @State private var renamingFile: RemoteFile?
    @State private var renameText = ""
    @State private var filesToMove: [RemoteFile] = []
    @State private var showMoveSheet = false
    @State private var moveDestinationText = ""
    @State private var filesToCopy: [RemoteFile] = []
    @State private var showCopySheet = false
    
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
            BreadcrumbView(path: viewModel.currentPath, rootPath: viewModel.remoteRoot) { path in
                Task { await viewModel.navigateTo(path: path) }
            }
            .padding(.horizontal)
            .padding(.vertical, 6)
            
            Divider()
            
            // File table
            if !viewModel.hasConnection {
                Spacer()
                VStack(spacing: 12) {
                    Image(systemName: "network.slash")
                        .font(.largeTitle)
                        .foregroundColor(.secondary)
                    Text("サーバーを選択して接続してください")
                        .foregroundColor(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                Spacer()
            } else if viewModel.isLoading {
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
                        Task { await viewModel.reload() }
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
                            Image(systemName: FileIcons.icon(for: file))
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
                    
                    TableColumn("権限", value: \.symbolicPermissions) { file in
                        Text(file.symbolicPermissions)
                            .font(.system(.body, design: .monospaced))
                            .foregroundColor(.secondary)
                    }
                    .width(min: 80, ideal: 100)
                }
                .contextMenu(forSelectionType: String.self) { selection in
                    let targets = files(in: selection)
                    if !targets.isEmpty {
                        fileContextMenu(targets: targets)
                    }
                } primaryAction: { selection in
                    let targets = files(in: selection)
                    if targets.count == 1, let file = targets.first {
                        Task { await viewModel.openFile(file) }
                    }
                }
            }
        }
        .searchable(text: $searchText, prompt: "ファイルを検索")
        .sheet(isPresented: $showNewFolderSheet) {
            NewFolderSheet(name: $newFolderName) {
                let name = newFolderName
                newFolderName = ""
                showNewFolderSheet = false
                Task {
                    await viewModel.createFolder(name: name)
                }
            }
        }
        .sheet(item: $renamingFile) { file in
            RenameSheet(name: $renameText) {
                let name = renameText
                renameText = ""
                renamingFile = nil
                Task {
                    await viewModel.renameFile(file, to: name)
                }
            }
        }
        .sheet(isPresented: $showMoveSheet, onDismiss: {
            filesToMove = []
            moveDestinationText = ""
        }) {
            MoveSheet(destination: $moveDestinationText) {
                let files = filesToMove
                let destination = moveDestinationText
                showMoveSheet = false
                Task {
                    await viewModel.moveFiles(files, to: destination)
                }
            }
        }
        .sheet(isPresented: $showCopySheet, onDismiss: {
            filesToCopy = []
            moveDestinationText = ""
        }) {
            MoveSheet(destination: $moveDestinationText, isCopy: true) {
                let files = filesToCopy
                let destination = moveDestinationText
                showCopySheet = false
                Task {
                    await viewModel.copyFiles(files, to: destination)
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
        .onChange(of: viewModel.files.map(\.id)) { _, currentIDs in
            selectedFiles.formIntersection(currentIDs)
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
                .disabled(!viewModel.canGoToParent)
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
            .disabled(!viewModel.hasConnection)
            
            Button(action: { Task { await viewModel.reload() } }) {
                Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)
            .help("更新")
            .disabled(!viewModel.hasConnection)
            
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
    private func fileContextMenu(targets: [RemoteFile]) -> some View {
        Group {
            // Opening and renaming act on one file. The selection is a Set, so
            // with several rows selected there is no "the" file to act on — this
            // used to take an arbitrary member and rename that one, which is not
            // the row the user right-clicked.
            if targets.count == 1, let file = targets.first {
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
            }

            Button("移動") {
                moveDestinationText = viewModel.currentPath
                filesToMove = targets
                showMoveSheet = true
            }

            Button("サーバー内で複製") {
                moveDestinationText = viewModel.currentPath
                filesToCopy = targets
                showCopySheet = true
            }

            Button("パスをコピー") {
                #if os(macOS)
                let pasteboard = NSPasteboard.general
                pasteboard.clearContents()
                pasteboard.setString(targets.map(\.path).joined(separator: "\n"), forType: .string)
                #endif
            }

            Divider()

            Menu("権限") {
                ForEach(["755", "644", "777", "600"], id: \.self) { mode in
                    Button(mode) {
                        Task { await viewModel.changePermissions(targets, mode: mode) }
                    }
                }
            }

            Divider()

            Button("削除", role: .destructive) {
                filesToDelete = targets
                showingDeleteConfirmation = true
            }
        }
    }

    /// The rows a menu acts on. SwiftUI hands the context menu the effective
    /// selection - right-clicking an unselected row yields just that row - so
    /// this is authoritative and preserves the on-screen order.
    private func files(in selection: Set<String>) -> [RemoteFile] {
        filteredFiles.filter { selection.contains($0.id) }
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
