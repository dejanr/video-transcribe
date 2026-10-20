import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from PySide6.QtCore import QSettings
from PySide6.QtTest import QTest
from PySide6.QtWidgets import QApplication

from ui.audio_subtitle import SubtitleOverlay, subtitle_settings


class OverlayTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.app = QApplication.instance() or QApplication(["test-overlay", "-style", "Fusion"])

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.settings = QSettings(str(Path(self.temp.name) / "settings.ini"), QSettings.Format.IniFormat)
        self.window = SubtitleOverlay("unused", [], demo=True, settings=self.settings)
        self.window.demo_timer.stop()
        self.window.show()
        self.app.processEvents()

    def tearDown(self):
        self.window.close()
        self.app.processEvents()
        self.temp.cleanup()

    def test_title_and_controls_share_the_top_bar(self):
        self.assertEqual(self.window.title.text(), "Audio transcribe subtitles")
        self.assertEqual(self.window.windowTitle(), "Audio transcribe subtitles")
        controls = [
            self.window.pause_button,
            self.window.follow_button,
            self.window.clear_button,
            self.window.font_size,
            self.window.opacity,
            self.window.close_button,
        ]
        self.window.resize(self.window.minimumWidth(), self.window.minimumHeight())
        self.app.processEvents()
        for control in controls:
            self.assertTrue(self.window.toolbar.isAncestorOf(control))
            self.assertTrue(self.window.toolbar.contentsRect().contains(control.geometry()))
        self.assertLess(self.window.toolbar.geometry().bottom(), self.window.history.geometry().top())

    def test_status_omits_repeated_language_labels(self):
        for kind in ("partial", "subtitle"):
            self.window.handle_event({"type": kind, "id": 1, "text": "Hello", "inference_seconds": 0.9})
            self.assertNotIn("DE → EN", self.window.status.text())
            self.assertNotIn("German → English", self.window.status.text())
            self.assertIn("0.9s processing", self.window.status.text())
        self.window.pause_button.setChecked(True)
        self.window.pause_button.setChecked(False)
        self.assertEqual(self.window.status.text(), "Listening")

    def test_runtime_and_vad_timing_are_visible(self):
        self.window.handle_event({"type": "status", "text": "Listening", "runtime": "Vulkan0"})
        self.window.handle_event({
            "type": "partial", "id": 1, "text": "Hello",
            "inference_seconds": 0.3, "vad_seconds": 0.02,
        })
        self.assertIn("Vulkan0", self.window.status.text())
        self.assertIn("VAD: 0.02s", self.window.status.toolTip())
        self.assertIn("0.3s processing", self.window.status.text())

    def test_long_status_keeps_toolbar_compact_and_details_accessible(self):
        self.window.resize(self.window.minimumWidth(), self.window.minimumHeight())
        self.app.processEvents()
        width = self.window.width()
        message = "Error · " + "Long backend diagnostic " * 20
        self.window.handle_event({"type": "error", "text": message})
        self.app.processEvents()
        self.assertEqual(self.window.width(), width)
        self.assertEqual(self.window.status.toolTip(), "Error · " + message)

    def test_settings_migrate_without_overwriting_new_preferences(self):
        previous = QSettings(str(Path(self.temp.name) / "previous.ini"), QSettings.Format.IniFormat)
        previous.setValue("font_size", 32)
        previous.setValue("background", 60)
        previous.setValue("geometry", self.window.saveGeometry())
        self.settings.setValue("font_size", 36)
        with patch("ui.audio_subtitle.QSettings", side_effect=[self.settings, previous]) as factory:
            result = subtitle_settings()
        self.assertIs(result, self.settings)
        factory.assert_any_call("video-transcribe", "audio-transcribe-subtitles")
        self.assertEqual(int(result.value("font_size")), 36)
        self.assertEqual(int(result.value("background")), 60)
        self.assertEqual(result.value("geometry"), previous.value("geometry"))

    def test_plain_text_captions_and_pause(self):
        self.window.handle_event({"type": "subtitle", "text": "<b>Hello</b>", "latency_seconds": 2})
        self.assertIn("<b>Hello</b>", self.window.history.toPlainText())
        self.window.pause_button.setChecked(True)
        self.window.handle_event({"type": "subtitle", "text": "must not appear"})
        self.assertNotIn("must not appear", self.window.history.toPlainText())
        self.window.pause_button.setChecked(False)
        self.window.handle_event({"type": "subtitle", "text": "resumed"})
        self.assertIn("resumed", self.window.history.toPlainText())

    def test_previews_are_revised_then_committed_once(self):
        self.window.handle_event({"type": "partial", "id": 1, "text": "Good"})
        self.window.handle_event({"type": "partial", "id": 1, "text": "Good evening"})
        self.assertEqual(self.window.preview.text(), "Good evening")
        self.assertEqual(self.window.history.toPlainText(), "")
        self.window.handle_event({"type": "subtitle", "id": 1, "text": "Good evening, everyone."})
        self.assertEqual(self.window.history.toPlainText().strip(), "Good evening, everyone.")
        self.assertEqual(self.window.preview.text(), "")
        self.assertFalse(self.window.preview.isVisible())

    def test_skipped_final_preserves_last_preview(self):
        self.window.handle_event({"type": "partial", "id": 1, "text": "Earlier sentence."})
        self.window.handle_event({"type": "partial", "id": 2, "text": "New sentence"})
        self.assertIn("Earlier sentence.", self.window.history.toPlainText())
        self.assertEqual(self.window.preview.text(), "New sentence")

    def test_pause_and_clear_discard_previews(self):
        self.window.handle_event({"type": "partial", "id": 1, "text": "Old preview"})
        self.window.pause_button.setChecked(True)
        self.window.handle_event({"type": "partial", "id": 1, "text": "Ignored preview"})
        self.assertEqual(self.window.preview.text(), "")
        self.window.pause_button.setChecked(False)
        self.window.handle_event({"type": "partial", "id": 2, "text": "New preview"})
        self.window.clear_history()
        self.assertEqual(self.window.preview.text(), "")
        self.assertEqual(self.window.history.toPlainText(), "")

    def test_empty_final_clears_preview(self):
        self.window.handle_event({"type": "partial", "id": 1, "text": "Provisional words"})
        self.window.handle_event({"type": "subtitle", "id": 1, "text": ""})
        self.assertEqual(self.window.preview.text(), "")
        self.assertEqual(self.window.history.toPlainText(), "")

    def test_previews_do_not_interrupt_scroll_back(self):
        for index in range(30):
            self.window.append_subtitle(f"Subtitle {index}")
        self.app.processEvents()
        scrollbar = self.window.history.verticalScrollBar()
        scrollbar.setValue(0)
        self.window.handle_event({"type": "partial", "id": 1, "text": "Current speech"})
        self.app.processEvents()
        self.assertEqual(scrollbar.value(), 0)
        self.assertFalse(self.window.preview.isVisible())
        self.window.follow_button.setChecked(True)
        self.app.processEvents()
        self.assertTrue(self.window.preview.isVisible())

    def test_previews_keep_live_view_at_bottom(self):
        for index in range(30):
            self.window.append_subtitle(f"Subtitle {index}")
        self.app.processEvents()
        for kind, text in [
            ("partial", "Current speech " * 12),
            ("partial", "Revised speech"),
            ("subtitle", "Final translation."),
        ]:
            self.window.handle_event({"type": kind, "id": 1, "text": text})
            self.app.processEvents()
            scrollbar = self.window.history.verticalScrollBar()
            self.assertTrue(self.window.follow_button.isChecked())
            self.assertEqual(scrollbar.value(), scrollbar.maximum())

    def test_scroll_back_does_not_jump_to_live(self):
        for index in range(30):
            self.window.append_subtitle(f"Subtitle number {index}")
        self.app.processEvents()
        scrollbar = self.window.history.verticalScrollBar()
        self.assertEqual(scrollbar.value(), scrollbar.maximum())
        scrollbar.setValue(0)
        self.assertFalse(self.window.follow_button.isChecked())
        self.window.append_subtitle("Another subtitle")
        self.assertEqual(scrollbar.value(), 0)
        self.window.follow_button.setChecked(True)
        self.assertEqual(scrollbar.value(), scrollbar.maximum())

    def test_history_is_bounded_and_clear_resets_follow(self):
        for index in range(1000):
            self.window.append_subtitle(f"Subtitle {index}")
        self.assertLessEqual(self.window.history.document().blockCount(), 800)
        self.window.clear_history()
        self.assertEqual(self.window.history.toPlainText(), "")
        self.assertTrue(self.window.follow_button.isChecked())

    def test_settings_persist(self):
        self.window.font_size.setValue(36)
        self.window.opacity.setValue(20)
        self.window.close()
        self.assertEqual(int(self.settings.value("font_size")), 36)
        self.assertEqual(int(self.settings.value("background")), 20)

    def test_backend_events_and_shutdown(self):
        helper = Path(self.temp.name) / "backend.py"
        helper.write_text(
            "import json, sys, time\n"
            "data = json.dumps({'type': 'subtitle', 'text': 'Local backend caption'}) + '\\n'\n"
            "sys.stdout.write(data[:12]); sys.stdout.flush(); time.sleep(0.05)\n"
            "sys.stdout.write(data[12:]); sys.stdout.flush()\n"
            "time.sleep(30)\n"
        )
        self.window.process.setProgram(sys.executable)
        self.window.process.setArguments([str(helper)])
        self.window.process.start()
        for _ in range(100):
            QTest.qWait(20)
            if "Local backend caption" in self.window.history.toPlainText():
                break
        self.assertIn("Local backend caption", self.window.history.toPlainText())
        self.window.close()
        self.assertEqual(self.window.process.state(), self.window.process.ProcessState.NotRunning)


if __name__ == "__main__":
    unittest.main()
