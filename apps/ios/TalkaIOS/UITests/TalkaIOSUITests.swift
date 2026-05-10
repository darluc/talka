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
        XCTAssertFalse(app.buttons["Discover"].exists)
        XCTAssertFalse(app.buttons["Forget Device"].exists)

        app.buttons["debugMenuButton"].tap()
        XCTAssertTrue(app.otherElements["debugPanel"].waitForExistence(timeout: 2))

        app.buttons["connectionPanelCloseButton"].tap()

        XCTAssertTrue(panel.waitForNonExistence(timeout: 2))
    }

    func testConnectedMacButtonShowsForgetDeviceConfirmation() {
        app.buttons["connectionPanelButton"].tap()
        XCTAssertTrue(app.otherElements["connectionPanel"].waitForExistence(timeout: 2))

        app.buttons["connectedMacInfo"].tap()

        XCTAssertTrue(app.staticTexts["是否遗忘设备"].waitForExistence(timeout: 2))
        XCTAssertTrue(app.buttons["遗忘设备"].exists)
    }

    func testPowerButtonTogglesBetweenConnectedAndDisconnected() {
        let powerButton = app.buttons["connectionPowerButton"]
        XCTAssertEqual(powerButton.value as? String, "connected")

        powerButton.tap()
        XCTAssertEqual(powerButton.value as? String, "disconnected")
        XCTAssertFalse(app.buttons["microphoneButton"].isEnabled)

        powerButton.tap()
        XCTAssertEqual(powerButton.value as? String, "connected")
        XCTAssertTrue(app.buttons["microphoneButton"].isEnabled)
    }

    func testMicrophonePressReleaseShowsProcessingState() {
        let microphone = app.buttons["microphoneButton"]

        microphone.press(forDuration: 0.2)

        XCTAssertEqual(microphone.value as? String, "processing")
    }
}
