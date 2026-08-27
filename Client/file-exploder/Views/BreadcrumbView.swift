import SwiftUI

struct BreadcrumbView: View {
    let path: String
    let rootPath: String
    let onNavigate: (String) -> Void
    
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
        let parts = relativePath.split(separator: "/").map(String.init)

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
                Button(action: {
                    onNavigate(RemotePath.standardized(rootPath))
                }) {
                    Image(systemName: "house")
                }
                .buttonStyle(.borderless)
                
                ForEach(Array(pathComponents.enumerated()), id: \.offset) { index, component in
                    Image(systemName: "chevron.right")
                        .font(.caption2)
                        .foregroundColor(.secondary)
                    
                    Button(action: { onNavigate(component.1) }) {
                        Text(component.0)
                    }
                    .buttonStyle(.borderless)
                }
            }
            .font(.callout)
        }
    }
}

#Preview {
    BreadcrumbView(path: "/home/user/documents", rootPath: "/home/user") { _ in }
}
