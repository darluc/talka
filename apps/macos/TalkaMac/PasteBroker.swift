import AppKit
import Darwin
import Foundation

final class LocalPasteBroker {
    private struct PasteRequest: Decodable {
        let op: String
        let key: String?
        let modifiers: [String]?
    }

    private struct PasteResponse: Encodable {
        let ok: Bool
        let error: String?
    }

    private let queue = DispatchQueue(label: "dev.talka.paste-broker", attributes: .concurrent)
    private var listenerFD: Int32 = -1

    let socketPath: String

    init(socketPath: String = "/tmp/talka-paste-\(getpid()).sock") {
        self.socketPath = socketPath
    }

    func start() throws {
        guard listenerFD == -1 else { return }

        unlink(socketPath)

        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else {
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }

        do {
            try bind(fd: fd, to: socketPath)
        } catch {
            close(fd)
            throw error
        }

        guard listen(fd, 16) == 0 else {
            let code = POSIXErrorCode(rawValue: errno) ?? .EIO
            close(fd)
            unlink(socketPath)
            throw POSIXError(code)
        }

        chmod(socketPath, S_IRUSR | S_IWUSR)
        listenerFD = fd
        queue.async { [weak self] in
            self?.acceptLoop()
        }
    }

    func stop() {
        guard listenerFD != -1 else { return }
        close(listenerFD)
        listenerFD = -1
        unlink(socketPath)
    }

    private func bind(fd: Int32, to path: String) throws {
        var address = sockaddr_un()
        address.sun_family = sa_family_t(AF_UNIX)

        let pathBytes = Array(path.utf8)
        let sunPathCapacity = MemoryLayout.size(ofValue: address.sun_path)
        let maxPathLength = sunPathCapacity - 1
        guard pathBytes.count <= maxPathLength else {
            throw POSIXError(.ENAMETOOLONG)
        }

        withUnsafeMutablePointer(to: &address.sun_path) { pointer in
            pointer.withMemoryRebound(to: CChar.self, capacity: sunPathCapacity) { buffer in
                for index in pathBytes.indices {
                    buffer[index] = CChar(bitPattern: pathBytes[index])
                }
                buffer[pathBytes.count] = 0
            }
        }

        let length = socklen_t(MemoryLayout<sa_family_t>.size + pathBytes.count + 1)
        let result = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPointer in
                Darwin.bind(fd, sockaddrPointer, length)
            }
        }
        guard result == 0 else {
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
    }

    private func acceptLoop() {
        while listenerFD != -1 {
            let clientFD = accept(listenerFD, nil, nil)
            if clientFD < 0 {
                if errno == EINTR {
                    continue
                }
                break
            }

            queue.async { [weak self] in
                self?.handle(clientFD: clientFD)
            }
        }
    }

    private func handle(clientFD: Int32) {
        guard let request = readRequest(from: clientFD),
              request.op == "paste" || request.op == "preflight" || request.op == "key_press" else {
            send(PasteResponse(ok: false, error: "bad_request"), to: clientFD)
            close(clientFD)
            return
        }

        guard AXIsProcessTrusted() else {
            send(PasteResponse(ok: false, error: "accessibility_missing"), to: clientFD)
            close(clientFD)
            return
        }

        guard request.op == "paste" else {
            if request.op == "key_press" {
                handleKeyPress(request, clientFD: clientFD)
                return
            }

            send(PasteResponse(ok: true, error: nil), to: clientFD)
            close(clientFD)
            return
        }

        DispatchQueue.main.async {
            Self.postCommandV()
            self.send(PasteResponse(ok: true, error: nil), to: clientFD)
            close(clientFD)
        }
    }

    private func handleKeyPress(_ request: PasteRequest, clientFD: Int32) {
        guard request.key == "enter" else {
            send(PasteResponse(ok: false, error: "bad_request"), to: clientFD)
            close(clientFD)
            return
        }

        let modifiers = request.modifiers ?? []
        guard let flags = Self.eventFlags(for: modifiers) else {
            send(PasteResponse(ok: false, error: "bad_request"), to: clientFD)
            close(clientFD)
            return
        }

        DispatchQueue.main.async {
            Self.postKeyPress(virtualKey: 36, flags: flags)
            self.send(PasteResponse(ok: true, error: nil), to: clientFD)
            close(clientFD)
        }
    }

    private func readRequest(from clientFD: Int32) -> PasteRequest? {
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)

        while data.count < 8_192 {
            let count = Darwin.read(clientFD, &buffer, buffer.count)
            if count > 0 {
                data.append(buffer, count: count)
                if buffer.prefix(count).contains(10) {
                    break
                }
                continue
            }
            if count == 0 {
                break
            }
            if errno == EINTR {
                continue
            }
            return nil
        }

        return try? JSONDecoder().decode(PasteRequest.self, from: data)
    }

    private func send(_ response: PasteResponse, to clientFD: Int32) {
        guard var data = try? JSONEncoder().encode(response) else { return }
        data.append(10)
        data.withUnsafeBytes { buffer in
            guard let baseAddress = buffer.baseAddress else { return }
            _ = Darwin.write(clientFD, baseAddress, buffer.count)
        }
    }

    private static func postCommandV() {
        postKeyPress(virtualKey: 9, flags: .maskCommand)
    }

    private static func postKeyPress(virtualKey: CGKeyCode, flags: CGEventFlags) {
        guard let source = CGEventSource(stateID: .hidSystemState),
              let keyDown = CGEvent(keyboardEventSource: source, virtualKey: virtualKey, keyDown: true),
              let keyUp = CGEvent(keyboardEventSource: source, virtualKey: virtualKey, keyDown: false) else {
            return
        }

        keyDown.flags = flags
        keyUp.flags = flags
        keyDown.post(tap: .cghidEventTap)
        keyUp.post(tap: .cghidEventTap)
    }

    private static func eventFlags(for modifiers: [String]) -> CGEventFlags? {
        var flags = CGEventFlags()
        for modifier in modifiers {
            switch modifier {
            case "cmd":
                flags.insert(.maskCommand)
            case "alt":
                flags.insert(.maskAlternate)
            case "shift":
                flags.insert(.maskShift)
            default:
                return nil
            }
        }
        return flags
    }
}
