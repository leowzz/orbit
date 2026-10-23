"""Desktop control panel and Windows tray entry point."""

import argparse
import copy
import os
from pathlib import Path
import sys

from PySide6.QtCore import QCoreApplication, QEvent, QLockFile, Qt, QTimer
from PySide6.QtGui import QAction, QColor, QFont, QIcon, QPainter, QPalette, QPen, QPixmap
from PySide6.QtWidgets import (
    QApplication, QCheckBox, QComboBox, QFileDialog, QFormLayout, QGroupBox,
    QHBoxLayout, QLabel, QLineEdit, QMainWindow, QMenu, QMessageBox,
    QPlainTextEdit, QPushButton, QScrollArea, QSizePolicy, QSpinBox,
    QSystemTrayIcon, QTabWidget, QVBoxLayout, QWidget,
)

from .controller import WorkerController
from .settings import delete_password, get_password, load_config, save_config, save_password, validate_config


def app_icon(color="#32736b"):
    icon = QIcon()
    for size in (24, 32, 48, 64, 128):
        pixmap = QPixmap(size, size)
        pixmap.fill(Qt.GlobalColor.transparent)
        painter = QPainter(pixmap)
        painter.setRenderHint(QPainter.RenderHint.Antialiasing)
        painter.setPen(Qt.PenStyle.NoPen)
        painter.setBrush(QColor(color))
        painter.drawRoundedRect(1, 1, size - 2, size - 2, size / 4, size / 4)
        painter.setPen(QPen(QColor("white"), size / 13, Qt.PenStyle.SolidLine, Qt.PenCapStyle.RoundCap))
        for x, height in ((.27, .2), (.42, .48), (.58, .68), (.73, .32)):
            painter.drawLine(int(size*x), int(size*(1-height)/2), int(size*x), int(size*(1+height)/2))
        painter.end()
        icon.addPixmap(pixmap)
    return icon


class WorkerWindow(QMainWindow):
    def __init__(self, config_path, controller=None, tray_available=None):
        super().__init__()
        self.config_path = Path(config_path).expanduser().resolve()
        self.config = load_config(self.config_path)
        if self.config_path.exists():
            # Validate before passing user-edited JSON values to Qt native APIs.
            validate_config(self.config)
        self.controller = controller or WorkerController(self)
        self._quitting = False
        self._tray_notice = False
        self._snapshot = {}
        self._credential_warning = ""
        self._stored_password = False
        self.setWindowTitle("Orbit ASR")
        self.setWindowIcon(app_icon())
        self.resize(840, 680)
        self.setMinimumSize(520, 480)
        self._build_ui()
        self._load_fields()
        self._build_tray(QSystemTrayIcon.isSystemTrayAvailable() if tray_available is None else tray_available)
        self.controller.changed.connect(self._state_changed)
        self.controller.snapshot.connect(self._update_snapshot)
        self.controller.log.connect(self.logs.appendPlainText)
        self.controller.finished.connect(self._on_finished)
        self._state_changed("stopped")
        if not self.config["mqtt"].get("host"):
            self.tabs.setCurrentIndex(1)
        if self._credential_warning:
            self.notice.setText(self._credential_warning)

    def _build_ui(self):
        root = QWidget()
        self.setCentralWidget(root)
        layout = QVBoxLayout(root)
        layout.setContentsMargins(22, 20, 22, 16)
        layout.setSpacing(16)
        header = QHBoxLayout()
        self.status_label = QLabel("已停止")
        self.status_label.setObjectName("status")
        self.status_label.setAccessibleName("转写运行状态")
        self.status_label.setWordWrap(True)
        header.addWidget(self.status_label, 1)
        self.start_button = QPushButton("启动")
        self.start_button.setObjectName("primary")
        self.start_button.clicked.connect(self.start_worker)
        self.stop_button = QPushButton("停止")
        self.stop_button.clicked.connect(self.controller.stop)
        header.addWidget(self.start_button)
        header.addWidget(self.stop_button)
        layout.addLayout(header)
        self.tabs = QTabWidget()
        layout.addWidget(self.tabs, 1)
        self._runtime_tab()
        self._settings_tab()
        self.notice = QLabel("关闭窗口后可在托盘中继续运行。")
        self.notice.setWordWrap(True)
        self.notice.setObjectName("muted")
        layout.addWidget(self.notice)

    def _runtime_tab(self):
        page = QWidget()
        box = QVBoxLayout(page)
        box.setContentsMargins(16, 16, 16, 16)
        details = QFormLayout()
        details.setHorizontalSpacing(24)
        self.model_status = QLabel("—")
        self.model_status.setWordWrap(True)
        self.device_status = QLabel("—")
        self.mqtt_status = QLabel("未连接")
        self.queue_status = QLabel("—")
        for title, value in (("模型", self.model_status), ("计算设备", self.device_status),
                             ("MQTT", self.mqtt_status), ("任务队列", self.queue_status)):
            value.setTextFormat(Qt.TextFormat.PlainText)
            value.setTextInteractionFlags(Qt.TextInteractionFlag.TextSelectableByMouse)
            details.addRow(title, value)
        box.addLayout(details)
        log_header = QHBoxLayout()
        log_header.addWidget(QLabel("运行日志"), 1)
        clear = QPushButton("清空")
        clear.clicked.connect(lambda: self.logs.clear())
        log_header.addWidget(clear)
        box.addLayout(log_header)
        self.logs = QPlainTextEdit()
        self.logs.setReadOnly(True)
        self.logs.setAccessibleName("运行日志")
        self.logs.setMaximumBlockCount(1500)
        self.logs.setLineWrapMode(QPlainTextEdit.LineWrapMode.WidgetWidth)
        log_font = QFont("Consolas" if os.name == "nt" else "Menlo")
        log_font.setPointSize(10)
        self.logs.setFont(log_font)
        box.addWidget(self.logs, 1)
        self.tabs.addTab(page, "运行")

    @staticmethod
    def _form(title, parent):
        group = QGroupBox(title)
        form = QFormLayout(group)
        form.setContentsMargins(16, 20, 16, 16)
        form.setSpacing(12)
        form.setFieldGrowthPolicy(QFormLayout.FieldGrowthPolicy.AllNonFixedFieldsGrow)
        form.setRowWrapPolicy(QFormLayout.RowWrapPolicy.WrapLongRows)
        parent.addWidget(group)
        return form

    def _settings_tab(self):
        page = QWidget()
        outer = QVBoxLayout(page)
        outer.setContentsMargins(0, 0, 0, 12)
        scroll = QScrollArea()
        scroll.setWidgetResizable(True)
        scroll.setFrameShape(QScrollArea.Shape.NoFrame)
        content = QWidget()
        self.settings_content = content
        fields = QVBoxLayout(content)
        fields.setContentsMargins(16, 16, 16, 12)
        fields.setSpacing(16)
        form = self._form("转写", fields)
        self.agent_id = QLineEdit()
        self.host_label = QLineEdit()
        self.model = QComboBox()
        self.model.setEditable(True)
        self.model.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Fixed)
        self.model.setMinimumWidth(240)
        self.model.addItems(["Qwen/Qwen3-ASR-0.6B", "Qwen/Qwen3-ASR-1.7B"])
        model_row = QHBoxLayout()
        model_row.addWidget(self.model, 1)
        model_browse = QPushButton("本地目录…")
        model_browse.clicked.connect(self._choose_model)
        model_row.addWidget(model_browse)
        self.device = QComboBox()
        for label, value in (("自动选择", "auto"), ("CPU", "cpu"), ("NVIDIA GPU（CUDA）", "cuda")):
            self.device.addItem(label, value)
        form.addRow("Worker ID", self.agent_id)
        form.addRow("主机名称", self.host_label)
        form.addRow("模型", model_row)
        form.addRow("计算设备", self.device)
        self.model.setAccessibleName("模型名称或本地目录")
        form = self._form("MQTT", fields)
        self.mqtt_host = QLineEdit()
        self.mqtt_host.setPlaceholderText("mqtt.example.internal")
        self.mqtt_port = QSpinBox()
        self.mqtt_port.setRange(1, 65535)
        self.tls = QCheckBox("启用 TLS")
        port_row = QHBoxLayout()
        port_row.addWidget(self.mqtt_port)
        port_row.addWidget(self.tls)
        port_row.addStretch()
        self.mqtt_port.setAccessibleName("MQTT 端口")
        self.username = QLineEdit()
        self.password = QLineEdit()
        self.password.setEchoMode(QLineEdit.EchoMode.Password)
        self.password.setPlaceholderText("留空时使用环境变量")
        self.remember_password = QCheckBox("保存密码到系统凭据库")
        self.ca_file = QLineEdit()
        self.ca_file.setPlaceholderText("留空时使用系统证书")
        ca_row = QHBoxLayout()
        ca_row.addWidget(self.ca_file, 1)
        browse_ca = QPushButton("浏览…")
        browse_ca.clicked.connect(self._choose_ca)
        ca_row.addWidget(browse_ca)
        self.ca_file.setAccessibleName("CA 证书")
        form.addRow("服务器", self.mqtt_host)
        form.addRow("端口", port_row)
        form.addRow("用户名", self.username)
        form.addRow("密码", self.password)
        form.addRow("", self.remember_password)
        form.addRow("CA 证书", ca_row)
        for group in content.findChildren(QGroupBox):
            group_form = group.layout()
            for row in range(group_form.rowCount()):
                item = group_form.itemAt(row, QFormLayout.ItemRole.LabelRole)
                if item and item.widget():
                    item.widget().setMinimumWidth(80)
        fields.addStretch()
        scroll.setWidget(content)
        outer.addWidget(scroll, 1)
        actions = QHBoxLayout()
        actions.setContentsMargins(16, 0, 16, 0)
        self.save_note = QLabel()
        self.save_note.setWordWrap(True)
        self.save_note.setObjectName("muted")
        actions.addWidget(self.save_note, 1)
        self.save_button = QPushButton("保存设置")
        self.save_button.clicked.connect(self.save_settings)
        actions.addWidget(self.save_button)
        outer.addLayout(actions)
        self.tabs.addTab(page, "设置")

    def _load_fields(self):
        self.agent_id.setText(self.config["agent_id"])
        self.host_label.setText(self.config["host_label"])
        self.model.setCurrentText(self.config["model"])
        self.device.setCurrentIndex(self.device.findData(self.config["device"]))
        mqtt = self.config["mqtt"]
        self.mqtt_host.setText(mqtt["host"])
        self.mqtt_port.setValue(mqtt["port"])
        self.tls.setChecked(mqtt["tls"])
        self.username.setText(mqtt.get("username", ""))
        self.ca_file.setText(mqtt.get("ca_file") or "")
        if mqtt.get("username"):
            try:
                password = get_password(self.config_path)
                if password:
                    self.password.setText(password)
                    self.remember_password.setChecked(True)
                    self._stored_password = True
            except ValueError as exc:
                self._credential_warning = str(exc)

    def _collect(self):
        config = copy.deepcopy(self.config)
        config.update(agent_id=self.agent_id.text().strip(), host_label=self.host_label.text().strip(),
                      model=self.model.currentText().strip(), device=self.device.currentData())
        config["mqtt"].update(host=self.mqtt_host.text().strip(), port=self.mqtt_port.value(),
                              tls=self.tls.isChecked(), username=self.username.text().strip())
        ca = self.ca_file.text().strip()
        if ca:
            config["mqtt"]["ca_file"] = ca
        else:
            config["mqtt"].pop("ca_file", None)
        validate_config(config)
        return config

    def save_settings(self):
        if self.controller.active:
            return False
        try:
            config = self._collect()
            save_config(self.config_path, config)
        except (ValueError, OSError) as exc:
            QMessageBox.warning(self, "无法保存设置", str(exc))
            self.tabs.setCurrentIndex(1)
            return False
        self.config = config
        self.notice.setText("设置已保存。")
        try:
            if self.remember_password.isChecked() and self.password.text():
                save_password(self.config_path, self.password.text())
                self._stored_password = True
            elif self._stored_password and not self.remember_password.isChecked():
                delete_password(self.config_path)
                self._stored_password = False
        except ValueError as exc:
            # The user can still run with a session-only password or environment.
            self.notice.setText(f"设置已保存；系统凭据未更新，当前输入仍可用于本次运行。{exc}")
        return True

    def start_worker(self):
        if self.controller.active or not self.save_settings():
            return
        password = self.password.text() or None
        mqtt = self.config["mqtt"]
        if mqtt.get("username") and not (password or os.environ.get(mqtt.get("password_env", "ORBIT_MQTT_PASSWORD"))):
            self.tabs.setCurrentIndex(1)
            QMessageBox.warning(self, "缺少 MQTT 密码", "请填写密码，或设置配置中指定的密码环境变量。")
            return
        self._snapshot = {}
        self.model_status.setText(self.config["model"])
        self.device_status.setText("检测中")
        self.queue_status.setText("读取中")
        self.mqtt_status.setText("等待连接")
        self.tabs.setCurrentIndex(0)
        self.controller.start(self.config_path, self.config, password)

    def _state_changed(self, state):
        active = state in ("starting", "running", "stopping")
        self.start_button.setEnabled(not active and not self._quitting)
        self.stop_button.setEnabled(active and state != "stopping")
        self.settings_content.setEnabled(not active)
        self.save_button.setEnabled(not active)
        self.save_note.setText("停止转写后可修改设置。" if active else "")
        self.start_action.setEnabled(self.start_button.isEnabled())
        self.stop_action.setEnabled(self.stop_button.isEnabled())
        labels = {"stopped": "已停止", "starting": "正在启动", "running": "正在启动", "stopping": "正在停止", "error": "启动或运行失败"}
        self.status_label.setText(labels.get(state, "正在运行"))
        if not active:
            self.mqtt_status.setText("未连接")
        self._refresh_status()

    def _update_snapshot(self, snapshot):
        self._snapshot = snapshot
        self.model_status.setText(str(snapshot.get("model") or self.config["model"]))
        device = snapshot.get("device")
        self.device_status.setText({"cpu": "CPU", "cuda": "NVIDIA GPU（CUDA）"}.get(device, "检测中"))
        if snapshot.get("mqtt_connected"):
            self.mqtt_status.setText("已连接 · 已订阅" if snapshot.get("subscribed") else "已连接 · 未完成订阅")
        else:
            self.mqtt_status.setText("未连接 · 等待重连" if self.controller.active else "未连接")
        queue = snapshot.get("queue", {})
        self.queue_status.setText(f"待转写 {queue.get('queued', 0)}   ·   处理中 {queue.get('processing', 0)}   ·   待回传 {queue.get('outbox', 0)}")
        self._refresh_status()

    def _refresh_status(self):
        if self.controller.state == "running":
            status = self._snapshot.get("status", "starting")
            labels = {"starting": "正在加载模型", "ready": "等待音频", "processing": "正在转写", "error": "运行异常", "offline": "正在停止"}
            self.status_label.setText(labels.get(status, "正在启动"))
            if status == "ready" and not self._snapshot.get("mqtt_connected"):
                self.status_label.setText("等待 MQTT 连接")
            elif status == "ready" and not self._snapshot.get("subscribed"):
                self.status_label.setText("等待 MQTT 订阅")
        tooltip = "Orbit ASR · " + self.status_label.text()
        self.tray.setToolTip(tooltip)
        self.tray_status.setText(self.status_label.text())

    def _build_tray(self, available):
        self.tray = QSystemTrayIcon(self.windowIcon(), self)
        menu = QMenu(self)
        self.tray_status = QAction("已停止", menu)
        self.tray_status.setEnabled(False)
        menu.addAction(self.tray_status)
        show = menu.addAction("打开窗口")
        show.triggered.connect(self.show_window)
        menu.addSeparator()
        self.start_action = menu.addAction("启动转写")
        self.start_action.triggered.connect(self.start_worker)
        self.stop_action = menu.addAction("停止转写")
        self.stop_action.triggered.connect(self.controller.stop)
        menu.addSeparator()
        quit_action = menu.addAction("退出")
        quit_action.triggered.connect(self.quit_app)
        self.tray.setContextMenu(menu)
        self.tray.activated.connect(self._tray_activated)
        if available:
            self.tray.show()
        else:
            self.notice.setText("当前桌面不支持托盘；关闭窗口将停止转写并退出。")

    def _tray_activated(self, reason):
        if reason in (QSystemTrayIcon.ActivationReason.Trigger, QSystemTrayIcon.ActivationReason.DoubleClick):
            self.show_window()

    def show_window(self):
        self.showNormal()
        self.raise_()
        self.activateWindow()

    def _choose_model(self):
        path = QFileDialog.getExistingDirectory(self, "选择本地模型目录")
        if path:
            self.model.setCurrentText(path)

    def _choose_ca(self):
        path, _ = QFileDialog.getOpenFileName(self, "选择 CA 证书", "", "证书 (*.pem *.crt);;所有文件 (*)")
        if path:
            self.ca_file.setText(path)

    def closeEvent(self, event):
        if self._quitting and not self.controller.active:
            event.accept()
            return
        event.ignore()
        if self.tray.isVisible() and not self._quitting:
            self.hide()
            if not self._tray_notice:
                self.tray.showMessage("Orbit ASR", "窗口已收起。可在托盘中打开窗口、停止转写或退出。")
                self._tray_notice = True
        else:
            self.quit_app()

    def quit_app(self):
        if self._quitting:
            return
        self._quitting = True
        if self.controller.active:
            self.controller.stop()
            self.start_button.setEnabled(False)
            self.start_action.setEnabled(False)
        else:
            self._on_finished()

    def _on_finished(self):
        if self._quitting:
            self.tray.hide()
            QApplication.instance().quit()


def configure_app(app):
    app.setApplicationName("Orbit ASR")
    app.setOrganizationName("Orbit")
    app.setQuitOnLastWindowClosed(False)
    app.setStyle("Fusion")
    palette = QPalette()
    for role, color in ((QPalette.ColorRole.Window, "#f5f6f8"),
                        (QPalette.ColorRole.WindowText, "#202d3a"),
                        (QPalette.ColorRole.Base, "#ffffff"),
                        (QPalette.ColorRole.Text, "#202d3a"),
                        (QPalette.ColorRole.Button, "#f1f3f5"),
                        (QPalette.ColorRole.ButtonText, "#202d3a"),
                        (QPalette.ColorRole.Highlight, "#32736b"),
                        (QPalette.ColorRole.HighlightedText, "#ffffff")):
        palette.setColor(role, QColor(color))
    palette.setColor(QPalette.ColorGroup.Disabled, QPalette.ColorRole.Text, QColor("#858e98"))
    palette.setColor(QPalette.ColorGroup.Disabled, QPalette.ColorRole.ButtonText, QColor("#858e98"))
    app.setPalette(palette)
    font = app.font()
    font.setPointSize(11)
    app.setFont(font)
    app.setStyleSheet("""
        QMainWindow, QTabWidget::pane, QScrollArea, QScrollArea > QWidget > QWidget { background: #f5f6f8; }
        QLabel#status { font-size: 23px; font-weight: 600; color: #202d3a; }
        QLabel#muted { color: #64717c; }
        QGroupBox { background: #ffffff; border: 1px solid #dce1e7; border-radius: 7px; margin-top: 12px; }
        QGroupBox::title { subcontrol-origin: margin; left: 14px; padding: 0 4px; }
        QPushButton { padding: 6px 16px; }
        QPushButton#primary { background: #32736b; color: white; border: 1px solid #32736b; border-radius: 5px; }
        QPushButton#primary:disabled { background: #b1c6c2; border-color: #b1c6c2; }
        QLineEdit, QComboBox, QSpinBox { padding: 5px; min-height: 22px; }
        QPlainTextEdit { background: #ffffff; border: 1px solid #dce1e7; border-radius: 5px; padding: 7px; }
        QTabBar::tab { padding: 8px 22px; }
    """)


def main():
    parser = argparse.ArgumentParser(description="Orbit ASR desktop controller")
    parser.add_argument("--config", default="config.json")
    parser.add_argument("--start", action="store_true", help="start the worker after opening")
    parser.add_argument("--minimized", action="store_true", help="start in the system tray")
    args = parser.parse_args()
    app = QApplication(sys.argv[:1])
    window = None
    lock = None
    try:
        configure_app(app)
        path = Path(args.config).expanduser().resolve()
        path.parent.mkdir(parents=True, exist_ok=True)
        lock = QLockFile(str(path) + ".gui.lock")
        lock.setStaleLockTime(0)
        if not lock.tryLock(0):
            QMessageBox.information(None, "Orbit ASR 已打开", "该配置的窗口已在运行，请从系统托盘打开。")
            return 1
        try:
            window = WorkerWindow(path)
        except (ValueError, OSError, KeyError, TypeError, OverflowError) as exc:
            QMessageBox.critical(None, "无法读取配置", f"请检查配置文件后重新打开。\n{exc}")
            return 1
        if not args.minimized or not window.tray.isVisible():
            window.show()
        if args.start:
            QTimer.singleShot(0, window.start_worker)
        return app.exec()
    finally:
        # Bound slots retain Python reference cycles. Dispose native widgets and
        # their controller/timers while the GUI/font backend is still alive,
        # rather than leaving their destruction to interpreter shutdown.
        if window is not None:
            window.tray.hide()
            window.hide()
            window.deleteLater()
        QCoreApplication.sendPostedEvents(None, QEvent.Type.DeferredDelete)
        if lock is not None:
            lock.unlock()
        app.shutdown()



if __name__ == "__main__":
    raise SystemExit(main())
