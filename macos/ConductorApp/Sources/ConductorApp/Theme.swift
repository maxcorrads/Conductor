import SwiftUI

enum ConductorTheme {
    static let signal = Color(red: 49 / 255, green: 87 / 255, blue: 213 / 255)
    static let waiting = Color(red: 216 / 255, green: 149 / 255, blue: 39 / 255)
    static let complete = Color(red: 46 / 255, green: 155 / 255, blue: 99 / 255)
    static let failure = Color(red: 207 / 255, green: 74 / 255, blue: 85 / 255)
    static let muted = Color.secondary.opacity(0.7)

    static func statusColor(connected: Bool, busy: Bool, failed: Bool = false) -> Color {
        if failed { return failure }
        if !connected { return Color.secondary.opacity(0.45) }
        return busy ? signal : complete
    }

    static func taskColor(_ status: String) -> Color {
        switch status {
        case "running": return signal
        case "finished": return complete
        case "failed": return failure
        case "cancelled": return muted
        default: return waiting
        }
    }

    static func taskSymbol(_ status: String) -> String {
        switch status {
        case "running": return "play.fill"
        case "finished": return "checkmark"
        case "failed": return "xmark"
        case "cancelled": return "minus"
        default: return "questionmark"
        }
    }
}

struct StatusDot: View {
    let color: Color
    var pulse = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var expanded = false

    var body: some View {
        ZStack {
            if pulse && !reduceMotion {
                Circle()
                    .fill(color.opacity(0.22))
                    .frame(width: 16, height: 16)
                    .scaleEffect(expanded ? 1.35 : 0.7)
                    .opacity(expanded ? 0 : 1)
            }
            Circle().fill(color).frame(width: 8, height: 8)
        }
        .frame(width: 18, height: 18)
        .onAppear {
            guard pulse && !reduceMotion else { return }
            withAnimation(.easeOut(duration: 1.25).repeatForever(autoreverses: false)) {
                expanded = true
            }
        }
        .accessibilityHidden(true)
    }
}

struct Panel<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        content
            .padding(16)
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .stroke(Color.primary.opacity(0.07))
            }
    }
}
