import XCTest

final class TalkaIOSUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        super.setUp()
        continueAfterFailure = false

        app = XCUIApplication()
        app.launchArguments = ["--ui-testing"]
        app.launch()
    }

    override func tearDown() {
        app = nil
        super.tearDown()
    }

    func testConnectionPanelCanOpenAndClose() {
        app.buttons["connectionPanelButton"].tap()

        let panel = app.otherElements["connectionPanel"]
        XCTAssertTrue(panel.waitForExistence(timeout: 2))
        XCTAssertFalse(app.buttons["PIN"].exists)

        app.buttons["Debug"].tap()
        XCTAssertTrue(app.otherElements["debugPanel"].waitForExistence(timeout: 2))

        app.buttons["connectionPanelCloseButton"].tap()

        XCTAssertTrue(panel.waitForNonExistence(timeout: 2))
    }

    func testPowerButtonTogglesBetweenConnectedAndDisconnected() {
        let powerButton = app.buttons["connectionPowerButton"]
        XCTAssertEqual(powerButton.value as? String, "connected")

        powerButton.tap()
        XCTAssertEqual(powerButton.value as? String, "disconnected")

        powerButton.tap()
        XCTAssertEqual(powerButton.value as? String, "connected")
    }

    func testMicrophonePressReleaseShowsProcessingState() {
        let microphone = app.buttons["microphoneButton"]

        microphone.press(forDuration: 0.2)

        XCTAssertEqual(microphone.value as? String, "processing")
    }
}
