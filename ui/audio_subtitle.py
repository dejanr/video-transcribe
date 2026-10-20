import argparse
import json
import os
import shutil
import signal
import subprocess
import sys

from PySide6.QtCore import QProcess, QSettings, Qt, QTimer
from PySide6.QtGui import QColor, QFont, QPainter
from PySide6.QtWidgets import (
    QApplication,
    QFrame,
    QHBoxLayout,
    QLabel,
    QPlainTextEdit,
    QPushButton,
    QSizeGrip,
    QSizePolicy,
    QSlider,
    QSpinBox,
    QVBoxLayout,
    QWidget,
)


def subtitle_settings():
    settings = QSettings("video-transcribe", "audio-transcribe-subtitles")
    previous = QSettings("video-transcribe", "audio-transcribe-subtitle")
    for key in ("geometry", "font_size", "background"):
        if not settings.contains(key) and previous.contains(key):
            settings.setValue(key, previous.value(key))
    return settings


class StatusLabel(QLabel):
    def paintEvent(self, event):
        painter = QPainter(self)
        painter.setFont(self.font())
        painter.setPen(self.palette().color(self.foregroundRole()))
        text = self.fontMetrics().elidedText(self.text(), Qt.TextElideMode.ElideRight, self.contentsRect().width())
        painter.drawText(self.contentsRect(), Qt.AlignmentFlag.AlignLeft | Qt.AlignmentFlag.AlignVCenter, text)


class DragHandle(QLabel):
    def mousePressEvent(self, event):
        if event.button() == Qt.MouseButton.LeftButton:
            handle = self.window().windowHandle()
            if handle:
                handle.startSystemMove()
        super().mousePressEvent(event)


class SubtitleOverlay(QWidget):
    def __init__(self, backend, backend_args, demo=False, settings=None):
        super().__init__()
        self.settings = settings if settings is not None else subtitle_settings()
        self.demo = demo
        self.paused = False
        self.closing = False
        self.updating = False
        self.pending = bytearray()
        self.diagnostics = ""
        self.preview_id = None
        self.runtime = ""
        self.background = int(self.settings.value("background", 45))
        self.setWindowTitle("Audio transcribe subtitles")
        self.setWindowFlags(
            Qt.WindowType.Dialog
            | Qt.WindowType.FramelessWindowHint
            | Qt.WindowType.WindowStaysOnTopHint
        )
        self.setAttribute(Qt.WidgetAttribute.WA_TranslucentBackground)
        self.resize(1000, 330)
        self.setStyleSheet("""
            QWidget { color: #e2e8f0; }
            QLabel { background: transparent; }
            QFrame#toolbar { background: rgba(15, 23, 35, 225); border-radius: 10px; }
            QLabel#controlLabel, QFrame#toolbar QPushButton, QFrame#toolbar QSpinBox {
                font-size: 12px;
            }
            QLabel#title { color: #f1f5f9; font-size: 14px; font-weight: 600; }
            QLabel#status { color: #94a3b8; font-size: 11px; }
            QLabel#controlLabel { color: #94a3b8; }
            QFrame#divider { background: rgba(148, 163, 184, 45); }
            QPushButton { background: rgba(51, 65, 85, 80); border: 1px solid transparent;
                          border-radius: 6px; padding: 6px 12px; min-height: 18px; }
            QPushButton:hover { background: rgba(71, 85, 105, 140); }
            QPushButton:pressed { background: rgba(71, 85, 105, 200); }
            QPushButton:focus { border-color: #5eead4; }
            QPushButton:checked { background: rgba(20, 184, 166, 35); color: #99f6e4;
                                  border-color: rgba(45, 212, 191, 70); }
            QPushButton#pause:checked { background: rgba(245, 158, 11, 30); color: #fde68a;
                                        border-color: rgba(251, 191, 36, 70); }
            QPushButton#close { color: #94a3b8; padding: 6px; }
            QPushButton#close:hover { background: rgba(239, 68, 68, 40); color: #fecaca; }
            QPlainTextEdit { background: transparent; border: none; color: #f1f5f9;
                             padding: 6px; selection-background-color: #115e59; }
            QSpinBox { background: rgba(51, 65, 85, 100); border: 1px solid rgba(148, 163, 184, 40);
                       border-radius: 6px; padding: 5px; }
            QSpinBox:focus { border-color: #5eead4; }
            QSlider::groove:horizontal { height: 4px; background: #334155; border-radius: 2px; }
            QSlider::sub-page:horizontal { background: #5d9792; border-radius: 2px; }
            QSlider::handle:horizontal { background: #cbd5e1; border-radius: 5px;
                                         width: 10px; margin: -3px 0; }
            QSlider::handle:horizontal:hover { background: #99f6e4; }
            QScrollBar:vertical { background: transparent; width: 6px; margin: 2px 0; }
            QScrollBar::handle:vertical { background: rgba(148, 163, 184, 90);
                                          border-radius: 3px; min-height: 24px; }
            QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical { height: 0; }
            QScrollBar::add-page:vertical, QScrollBar::sub-page:vertical { background: transparent; }
        """)
        layout = QVBoxLayout(self)
        layout.setContentsMargins(12, 12, 12, 14)
        layout.setSpacing(12)
        self.toolbar = QFrame()
        self.toolbar.setObjectName("toolbar")
        toolbar = QHBoxLayout(self.toolbar)
        toolbar.setContentsMargins(14, 10, 10, 10)
        toolbar.setSpacing(6)
        title_group = QVBoxLayout()
        title_group.setSpacing(3)
        self.title = DragHandle("Audio transcribe subtitles")
        self.title.setObjectName("title")
        self.title.setToolTip("Drag to move the overlay")
        title_group.addWidget(self.title)
        self.status = StatusLabel("Starting local translation…")
        self.status.setObjectName("status")
        self.status.setTextFormat(Qt.TextFormat.PlainText)
        self.status.setSizePolicy(QSizePolicy.Policy.Ignored, QSizePolicy.Policy.Fixed)
        title_group.addWidget(self.status)
        toolbar.addLayout(title_group, 1)
        toolbar.addSpacing(12)

        self.pause_button = QPushButton("Pause")
        self.pause_button.setObjectName("pause")
        self.pause_button.setCheckable(True)
        self.pause_button.setToolTip("Pause subtitles and discard incoming audio")
        self.pause_button.toggled.connect(self.set_paused)
        toolbar.addWidget(self.pause_button)
        self.follow_button = QPushButton("Live")
        self.follow_button.setCheckable(True)
        self.follow_button.setChecked(True)
        self.follow_button.setToolTip("Follow the newest subtitles")
        self.follow_button.toggled.connect(self.follow_live)
        toolbar.addWidget(self.follow_button)
        self.clear_button = QPushButton("Clear")
        self.clear_button.setToolTip("Clear subtitle history")
        self.clear_button.clicked.connect(self.clear_history)
        toolbar.addWidget(self.clear_button)
        divider = QFrame()
        divider.setObjectName("divider")
        divider.setFixedSize(1, 24)
        toolbar.addSpacing(6)
        toolbar.addWidget(divider)
        toolbar.addSpacing(6)

        size_label = QLabel("Size")
        size_label.setObjectName("controlLabel")
        toolbar.addWidget(size_label)
        self.font_size = QSpinBox()
        self.font_size.setRange(16, 60)
        self.font_size.setValue(int(self.settings.value("font_size", 28)))
        self.font_size.setFixedWidth(58)
        self.font_size.setToolTip("Subtitle font size")
        self.font_size.setAccessibleName("Subtitle font size")
        self.font_size.valueChanged.connect(self.update_font)
        size_label.setBuddy(self.font_size)
        toolbar.addWidget(self.font_size)
        toolbar.addSpacing(8)
        opacity_label = QLabel("Background")
        opacity_label.setObjectName("controlLabel")
        toolbar.addWidget(opacity_label)
        self.opacity = QSlider(Qt.Orientation.Horizontal)
        self.opacity.setRange(0, 95)
        self.opacity.setValue(self.background)
        self.opacity.setFixedWidth(76)
        self.opacity.setToolTip("Background opacity")
        self.opacity.setAccessibleName("Background opacity")
        self.opacity.valueChanged.connect(self.update_background)
        opacity_label.setBuddy(self.opacity)
        toolbar.addWidget(self.opacity)
        toolbar.addSpacing(8)
        self.close_button = QPushButton("×")
        self.close_button.setObjectName("close")
        self.close_button.setFixedWidth(32)
        self.close_button.setToolTip("Close and stop audio capture")
        self.close_button.setAccessibleName("Close")
        self.close_button.clicked.connect(self.close)
        toolbar.addWidget(self.close_button)
        layout.addWidget(self.toolbar)
        self.setMinimumSize(self.toolbar.minimumSizeHint().width() + 24, 200)

        self.history = QPlainTextEdit()
        self.history.setReadOnly(True)
        self.history.setMaximumBlockCount(800)
        self.history.setPlaceholderText("Subtitles will appear here. Scroll back to review, or click Live to catch up.")
        self.history.verticalScrollBar().valueChanged.connect(self.scrolled)
        self.history.verticalScrollBar().rangeChanged.connect(self.scroll_range_changed)
        layout.addWidget(self.history, 1)
        self.preview = QLabel()
        self.preview.setWordWrap(True)
        self.preview.setTextFormat(Qt.TextFormat.PlainText)
        self.preview.setContentsMargins(6, 0, 6, 6)
        self.preview.setStyleSheet("color: #b8e9e5; background: transparent;")
        self.preview.setToolTip("Live preview · translation may change as more speech arrives")
        self.preview.hide()
        layout.addWidget(self.preview)
        self.resize_grip = QSizeGrip(self)
        self.resize_grip.setFixedSize(16, 16)
        self.resize_grip.setToolTip("Drag to resize")
        self.update_font(self.font_size.value())

        geometry = self.settings.value("geometry")
        if geometry:
            self.restoreGeometry(geometry)
        else:
            available = QApplication.primaryScreen().availableGeometry()
            self.move(available.center().x() - self.width() // 2, available.bottom() - self.height() - 40)
        if not any(self.frameGeometry().intersects(screen.availableGeometry()) for screen in QApplication.screens()):
            self.move(QApplication.primaryScreen().availableGeometry().topLeft())

        self.process = QProcess(self)
        self.process.setProgram(backend)
        self.process.setArguments(backend_args)
        self.process.readyReadStandardOutput.connect(self.read_events)
        self.process.readyReadStandardError.connect(self.read_diagnostics)
        self.process.errorOccurred.connect(self.process_error)
        self.process.finished.connect(self.process_finished)
        self.demo_timer = QTimer(self)
        self.demo_timer.timeout.connect(self.demo_caption)
        self.demo_index = 0
        if demo:
            self.set_status("Demo mode", "No audio capture or model required.")
            self.demo_timer.start(1800)
        else:
            self.process.start()

    def resizeEvent(self, event):
        super().resizeEvent(event)
        self.resize_grip.move(self.width() - 18, self.height() - 18)

    def paintEvent(self, event):
        painter = QPainter(self)
        painter.setRenderHint(QPainter.RenderHint.Antialiasing)
        painter.setPen(QColor(148, 163, 184, 65))
        painter.setBrush(QColor(8, 12, 20, round(self.background * 2.55)))
        painter.drawRoundedRect(self.rect().adjusted(1, 1, -1, -1), 16, 16)

    def update_font(self, size):
        font = QFont("Sans Serif", size)
        font.setWeight(QFont.Weight.Medium)
        self.history.setFont(font)
        self.preview.setFont(font)

    def update_background(self, value):
        self.background = value
        self.update()

    def scrolled(self, value):
        if not self.updating:
            self.follow_button.setChecked(value >= self.history.verticalScrollBar().maximum() - 3)

    def scroll_range_changed(self, minimum, maximum):
        if self.follow_button.isChecked():
            self.history.verticalScrollBar().setValue(maximum)

    def follow_live(self, enabled):
        self.preview.setVisible(enabled and bool(self.preview.text()))
        if enabled:
            scrollbar = self.history.verticalScrollBar()
            scrollbar.setValue(scrollbar.maximum())

    def clear_history(self):
        self.clear_preview()
        self.history.clear()
        self.follow_button.setChecked(True)

    def set_paused(self, paused):
        self.paused = paused
        if paused:
            self.clear_preview()
        self.pause_button.setText("Resume" if paused else "Pause")
        if self.process.state() == QProcess.ProcessState.Running:
            self.process.write(b"pause\n" if paused else b"resume\n")
        self.set_status("Paused" if paused else "Listening", "Incoming audio is discarded while paused." if paused else None)

    def append_subtitle(self, text):
        if self.paused:
            return
        scrollbar = self.history.verticalScrollBar()
        previous = scrollbar.value()
        follow = self.follow_button.isChecked()
        self.updating = True
        self.history.appendPlainText(text + "\n")
        scrollbar.setValue(scrollbar.maximum() if follow else previous)
        self.updating = False

    def clear_preview(self):
        self.preview.clear()
        self.preview.hide()
        self.preview_id = None

    def update_caption(self, event):
        caption_id = event.get("id")
        if self.preview.text() and caption_id != self.preview_id:
            self.append_subtitle(self.preview.text())
            self.clear_preview()
        if event["type"] == "partial":
            self.preview_id = caption_id
            self.preview.setText(event["text"])
            self.preview.setVisible(self.follow_button.isChecked() and bool(event["text"]))
        else:
            self.clear_preview()
            if event["text"]:
                self.append_subtitle(event["text"])

    def read_events(self):
        self.pending.extend(bytes(self.process.readAllStandardOutput()))
        while b"\n" in self.pending:
            line, _, remainder = self.pending.partition(b"\n")
            self.pending = bytearray(remainder)
            try:
                event = json.loads(line)
            except (ValueError, UnicodeDecodeError):
                continue
            self.handle_event(event)

    def set_status(self, text, detail=None):
        self.status.setText(text)
        self.status.setToolTip(detail or text)

    def handle_event(self, event):
        kind = event.get("type")
        self.runtime = event.get("runtime", self.runtime)
        text = event.get("text", "")
        if kind == "error":
            self.set_status("Error · " + text)
        elif kind in ("partial", "subtitle") and not self.paused:
            self.update_caption(event)
            processing = event.get("inference_seconds", 0) + event.get("queue_seconds", 0) + event.get("vad_seconds", 0)
            mode = "Live preview" if kind == "partial" else "Listening"
            backend = f" · {self.runtime}" if self.runtime else ""
            self.set_status(
                f"{mode}{backend} · {processing:.1f}s processing",
                f"Audio window: {event.get('audio_seconds', 0):.1f}s · "
                f"Inference: {event.get('inference_seconds', 0):.1f}s · "
                f"VAD: {event.get('vad_seconds', 0):.2f}s · "
                f"Queue: {event.get('queue_seconds', 0):.1f}s · "
                f"Window start to caption: {event.get('latency_seconds', 0):.1f}s"
            )
        elif kind == "status" and not self.paused:
            self.set_status(text)

    def read_diagnostics(self):
        data = bytes(self.process.readAllStandardError()).decode("utf-8", errors="replace")
        self.diagnostics = (self.diagnostics + data)[-16000:]
        sys.stderr.write(data)

    def process_error(self, error):
        if not self.closing:
            self.set_status("Backend error · " + self.process.errorString())

    def process_finished(self, exit_code, exit_status):
        self.read_events()
        self.read_diagnostics()
        if not self.closing and not self.status.text().startswith("Error"):
            self.set_status(f"Translation stopped (exit {exit_code}). See terminal for details.")

    def demo_caption(self):
        if self.paused:
            return
        lines = [
            "Welcome. These English subtitles stay above your meeting or movie.",
            "All speech recognition and translation run on your own machine.",
            "Scroll up to review earlier dialogue, then click Live to catch up.",
        ]
        self.append_subtitle(lines[self.demo_index % len(lines)])
        self.demo_index += 1

    def closeEvent(self, event):
        self.closing = True
        self.demo_timer.stop()
        self.settings.setValue("geometry", self.saveGeometry())
        self.settings.setValue("font_size", self.font_size.value())
        self.settings.setValue("background", self.background)
        if self.process.state() != QProcess.ProcessState.NotRunning:
            self.process.terminate()
            if not self.process.waitForFinished(10000):
                self.process.kill()
                self.process.waitForFinished(2000)
        event.accept()


def main():
    parser = argparse.ArgumentParser(prog="audio-transcribe-subtitles", description="Local German → English system-audio subtitles.")
    parser.add_argument("--model", default="small", help="Multilingual Whisper model; default: small")
    parser.add_argument("--source", default="", help="PulseAudio output monitor; default: current output")
    parser.add_argument("--chunk-seconds", type=float, default=6, help="Maximum audio context window")
    parser.add_argument("--step-seconds", type=float, default=1.5, help="Preview interval; 0 disables previews")
    parser.add_argument("--overlap-seconds", type=float, default=0.75, help="Overlap across windows, 0–1 seconds")
    parser.add_argument("--vad-model", default="", help="Custom Silero GGML model path")
    parser.add_argument("--no-vad", action="store_true", help="Disable Silero VAD; keep energy gating")
    parser.add_argument("--vad-threshold", type=float, default=0.5, help="Silero speech probability threshold")
    parser.add_argument("--full-context", action="store_true", help="Full Whisper encoder context (slower)")
    parser.add_argument("--silence-threshold", type=float, default=0.008)
    parser.add_argument("--threads", type=int, default=4)
    parser.add_argument("--no-gpu", action="store_true")
    parser.add_argument("--list-sources", action="store_true")
    parser.add_argument("--demo", action="store_true", help="Preview the overlay without audio capture")
    parser.add_argument("--backend", default=os.environ.get("AUDIO_SUBTITLE_BACKEND", "audio-subtitle-stream"))
    args = parser.parse_args()
    if args.list_sources:
        return subprocess.call([args.backend, "--list-sources"])
    if not args.demo and not shutil.which(args.backend):
        parser.error("audio-subtitle-stream not found; use the Nix app or pass --backend /path/to/binary")
    backend_args = [
        "--model", args.model,
        "--source", args.source,
        "--chunk-seconds", str(args.chunk_seconds),
        "--step-seconds", str(args.step_seconds),
        "--overlap-seconds", str(args.overlap_seconds),
        "--vad-threshold", str(args.vad_threshold),
        "--vad-model", args.vad_model,
        "--silence-threshold", str(args.silence_threshold),
        "--threads", str(args.threads),
    ]
    if args.no_vad:
        backend_args.append("--no-vad")
    if args.full_context:
        backend_args.append("--full-context")
    if args.no_gpu:
        backend_args.append("--no-gpu")
    app = QApplication([sys.argv[0], "-style", "Fusion"])
    app.setApplicationName("audio-transcribe-subtitles")
    overlay = SubtitleOverlay(args.backend, backend_args, args.demo)
    overlay.show()
    signal.signal(signal.SIGINT, lambda *_: overlay.close())
    signal.signal(signal.SIGTERM, lambda *_: overlay.close())
    interrupt_timer = QTimer()
    interrupt_timer.timeout.connect(lambda: None)
    interrupt_timer.start(250)
    return app.exec()


if __name__ == "__main__":
    sys.exit(main())
