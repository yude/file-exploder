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
        // Computed once per render and reused below for both the empty-state
        // check and the table's rows, instead of letting each access to
        // filteredFiles redo its filter, and this sort, from scratch.
        let visibleFiles = filteredFiles.sorted(using: sortOrder)

        VStack(spacing: 0) {
            // Toolbar
            toolbar
            
            Divider()
            
            // Breadcrumb
            BreadcrumbView(
                path: viewModel.currentPath,
                rootPath: viewModel.remoteRoot,
                onNavigate: { path in
                    Task { await viewModel.navigateTo(path: path) }
                },
                onDrop: { dropped, destination in
                    move(dropped, to: destination)
                }
            )
            .padding(.horizontal)
            .padding(.vertical, 6)
            
            Divider()

            // An operation that failed belongs beside the listing, not instead
            // of it: the directory loaded fine, one action within it did not.
            // Routing these through the same full-page error meant a single
            // failed delete hid every file until the user navigated away.
            if let operationError = viewModel.errors.operationMessage {
                HStack(alignment: .top, spacing: 8) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundColor(.orange)
                    Text(operationError)
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .textSelection(.enabled)
                    Spacer(minLength: 0)
                    Button(action: { viewModel.dismissOperationErrors() }) {
                        Image(systemName: "xmark")
                    }
                    .buttonStyle(.borderless)
                    .help("閉じる")
                }
                .padding(.horizontal)
                .padding(.vertical, 6)

                Divider()
            }

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
            } else if let listingError = viewModel.errors.listingError {
                Spacer()
                VStack(spacing: 12) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.largeTitle)
                        .foregroundColor(.orange)
                    Text(listingError)
                        .foregroundColor(.secondary)
                    Button("再試行") {
                        Task { await viewModel.reload() }
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if visibleFiles.isEmpty {
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
                Table(of: RemoteFile.self, selection: $selectedFiles, sortOrder: $sortOrder) {
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
                } rows: {
                    // Dragging belongs on the row, not on its cells. Putting it
                    // on the cells placed a gesture in front of the table's own
                    // handling and cost selection and double-click entirely.
                    ForEach(visibleFiles) { file in
                        if file.isDirectory {
                            TableRow(file)
                                .draggable(DraggedRemoteFile(path: file.path))
                                .dropDestination(for: DraggedRemoteFile.self) { dropped in
                                    _ = move(dropped, to: file.path)
                                }
                        } else {
                            TableRow(file)
                                .draggable(DraggedRemoteFile(path: file.path))
                        }
                    }
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
        .onChange(of: viewModel.files) { _, currentFiles in
            selectedFiles.formIntersection(currentFiles.map(\.id))
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
                .disabled(!viewModel.canGoBack || viewModel.isLoading)

                Button(action: { Task { await viewModel.goForward() } }) {
                    Image(systemName: "chevron.right")
                }
                .disabled(!viewModel.canGoForward || viewModel.isLoading)

                Button(action: { Task { await viewModel.goToParent() } }) {
                    Image(systemName: "chevron.up")
                }
                .disabled(!viewModel.canGoToParent || viewModel.isLoading)
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
            .disabled(!viewModel.hasConnection || viewModel.isLoading)

            Button(action: { Task { await viewModel.reload() } }) {
                Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.borderless)
            .help("更新")
            .disabled(!viewModel.hasConnection || viewModel.isLoading)
            
            Spacer()
            
            // Selection info. Counted against the rows on screen, not against
            // the raw selection: a search can hide rows that stay selected, and
            // every action goes through files(in:), which only ever sees what is
            // displayed. Counting the rest would promise more than any menu item
            // would then do.
            let selectionOnScreen = files(in: selectedFiles).count
            if selectionOnScreen > 0 {
                Text("\(selectionOnScreen) 項目を選択中")
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

    // MARK: - Drag and drop

    /// Moves the dropped rows into `destination`, and reports whether it took
    /// them - returning false leaves the system showing the drag as rejected.
    private func move(_ dropped: [DraggedRemoteFile], to destination: String) -> Bool {
        // Resolve against the rows on screen. A drag that started outside the
        // app arrives as text that matches nothing and is declined here.
        //
        // Matched by identity, not by path: a Set<String> of paths compares its
        // members canonically, so dragging one of an NFC/NFD pair would pick up
        // both rows and move a file the user never touched.
        let droppedIDs = Set(dropped.map { RemoteFile.identity(for: $0.path) })
        var sources = filteredFiles.filter { droppedIDs.contains($0.id) }
        guard !sources.isEmpty else { return false }

        // Dragging one row of a selection takes the whole selection, the way
        // Finder does. Dragging an unselected row takes only that row.
        if sources.count == 1, let only = sources.first, selectedFiles.contains(only.id) {
            let selected = files(in: selectedFiles)
            if !selected.isEmpty {
                sources = selected
            }
        }

        let movable = sources.filter { RemotePath.canMove($0.path, into: destination) }
        guard !movable.isEmpty else { return false }

        if movable.count < sources.count {
            let movableIDs = Set(movable.map(\.id))
            for skipped in sources where !movableIDs.contains(skipped.id) {
                viewModel.reportOperationError("移動エラー (\(skipped.name)): 移動先に移動できないため対象から除外しました")
            }
        }

        Task { await viewModel.moveFiles(movable, to: destination) }
        return true
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
