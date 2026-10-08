import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'connection_config.dart';

class ScanConnectionScreen extends StatefulWidget {
  const ScanConnectionScreen({super.key});

  @override
  State<ScanConnectionScreen> createState() => _ScanConnectionScreenState();
}

class _ScanConnectionScreenState extends State<ScanConnectionScreen> {
  String? error;
  bool completed = false;

  void detect(BarcodeCapture capture) {
    if (completed || !mounted) return;
    for (final barcode in capture.barcodes) {
      final value = barcode.rawValue;
      if (value == null) continue;
      try {
        final config = ConnectionConfig.fromQr(value);
        completed = true;
        Navigator.pop(context, config);
        return;
      } on FormatException catch (e) {
        if (error != e.message) setState(() => error = e.message);
      }
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('扫码填写')),
    body: Column(
      children: [
        Expanded(
          child: MobileScanner(
            // Let the scanner own the camera lifecycle and release it on exit.
            onDetect: detect,
            errorBuilder: (context, exception) => Center(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.no_photography_outlined, size: 40),
                    const SizedBox(height: 16),
                    Text(
                      exception.errorCode ==
                              MobileScannerErrorCode.permissionDenied
                          ? '未获得相机权限。可在系统设置中允许 Orbit 使用相机后重试，或返回手动填写。'
                          : '无法打开相机，请返回重试或手动填写。',
                      textAlign: TextAlign.center,
                    ),
                    TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('返回填写'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
        SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text(
              error ?? '对准控制台「连接 App」中的二维码',
              textAlign: TextAlign.center,
              style: TextStyle(
                color: error == null
                    ? null
                    : Theme.of(context).colorScheme.error,
              ),
            ),
          ),
        ),
      ],
    ),
  );
}
