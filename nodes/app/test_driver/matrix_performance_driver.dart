import 'dart:convert';
import 'dart:io';
import 'package:integration_test/integration_test_driver_extended.dart';

Future<void> main() async {
  final label = Platform.environment['MATRIX_PERF_LABEL'] ?? 'current';
  final dir = Directory('build/matrix-performance/$label')
    ..createSync(recursive: true);
  await integrationDriver(
    onScreenshot: (name, bytes, [args]) async {
      await File('${dir.path}/$name.png').writeAsBytes(bytes);
      return true;
    },
    responseDataCallback: (data) async {
      final metrics = {...?data}..remove('screenshots');
      await File(
        '${dir.path}/timings.json',
      ).writeAsString(const JsonEncoder.withIndent('  ').convert(metrics));
    },
  );
}
