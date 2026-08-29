import SwiftUI

struct BreadcrumbView: View {
    let path: String
    let rootPath: String
    let onNavigate: (String) -> Void
    /// Handles rows dropped on a crumb, and reports whether it took them.
    /// Moving something up a level is otherwise unreachable by dragging: the
    /// parent directory has no row of its own to aim at.
    let onDrop: ([DraggedRemoteFile], String) -> Bool

    @State private var dropTarget: String?
    
    var pathComponents: [(String, String)] {
        var components: [(String, String)] = []
        let root = RemotePath.standardized(rootPath)
        let current = RemotePath.standardized(path)
        // Navigation is confined to the root, but a mid-flight reconnection can
        // still render this with the two out of step; show just the root rather
        // than slicing a path that is not underneath it.
        guard RemotePath.isDescendant(current, of: root) else { return [] }

        let relativePath: String
        if root == "/" {
            relativePath = String(current.dropFirst())
        } else if current == root {
            relativePath = ""
        } else {
            relativePath = String(current.dropFirst(root.count + 1))
        }
        // RemotePath's own scalar-based splitting, not split(separator: "/"):
        // a "/" immediately followed by a combining mark fuses into one
        // Character that isn't "/", the same hazard RemotePath.swift had to
        // fix in its own splitting.
        let parts = RemotePath.splitComponents(relativePath)

        var currentPath = root
        for part in parts {
            currentPath = RemotePath.appending(part, to: currentPath)
            components.append((part, currentPath))
        }
        
        return components
    }
    
    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 4) {
                // Root
                let root = RemotePath.standardized(rootPath)
                Button(action: { onNavigate(root) }) {
                    Image(systemName: "house")
                }
                .buttonStyle(.borderless)
                .modifier(CrumbDropTarget(destination: root, dropTarget: $dropTarget, onDrop: onDrop))
                
                ForEach(Array(pathComponents.enumerated()), id: \.offset) { index, component in
                    Image(systemName: "chevron.right")
                        .font(.caption2)
                        .foregroundColor(.secondary)
                    
                    Button(action: { onNavigate(component.1) }) {
                        Text(component.0)
                    }
                    .buttonStyle(.borderless)
                    .modifier(CrumbDropTarget(destination: component.1, dropTarget: $dropTarget, onDrop: onDrop))
                }
            }
            .font(.callout)
        }
    }
}

/// Makes one crumb a drop target and tints it while a drag is over it.
private struct CrumbDropTarget: ViewModifier {
    let destination: String
    @Binding var dropTarget: String?
    let onDrop: ([DraggedRemoteFile], String) -> Bool

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 4)
            .background(
                RoundedRectangle(cornerRadius: 4)
                    .fill(dropTarget == destination ? Color.accentColor.opacity(0.25) : Color.clear)
            )
            .dropDestination(for: DraggedRemoteFile.self) { dropped, _ in
                dropTarget = nil
                return onDrop(dropped, destination)
            } isTargeted: { targeted in
                if targeted {
                    dropTarget = destination
                } else if dropTarget == destination {
                    dropTarget = nil
                }
            }
    }
}

#Preview {
    BreadcrumbView(
        path: "/home/user/documents",
        rootPath: "/home/user",
        onNavigate: { _ in },
        onDrop: { _, _ in false }
    )
}
