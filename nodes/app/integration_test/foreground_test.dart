import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:integration_test/integration_test.dart';
import 'package:orbit_app/main.dart' as app;
import 'package:orbit_app/inbox_screen.dart';
import 'package:path_provider/path_provider.dart';
import 'package:uuid/uuid.dart';

// Run only against the disposable server provided by scripts/app-smoke.py.
void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  const server = String.fromEnvironment(
    'ORBIT_TEST_SERVER',
    defaultValue: 'http://10.0.2.2:17622',
  );
  const token = 'orbit-integration-test-token-00000001';
  testWidgets(
    'Android foreground sync, offline queue, todo, conflict and image',
    (tester) async {
      app.main();
      Future<void> waitFor(Finder finder) async {
        for (var i = 0; i < 60 && finder.evaluate().isEmpty; i++) {
          await tester.pump(const Duration(milliseconds: 250));
        }
        expect(finder, findsWidgets);
      }

      await waitFor(find.byType(TextField));
      await tester.enterText(find.byType(TextField).at(0), server);
      await tester.enterText(find.byType(TextField).at(1), token);
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(
        find.text('连接'),
        200,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.tap(find.text('连接'));
      await waitFor(find.byType(InboxScreen));
      final c = tester.widget<InboxScreen>(find.byType(InboxScreen)).controller;
      await c.sync();
      await tester.pumpAndSettle();
      expect(c.online, isTrue);
      expect(c.items.length, greaterThanOrEqualTo(2));
      expect(tester.takeException(), isNull);

      c.stop();
      await tester.enterText(find.byType(TextField), 'Android offline sample');
      await tester.tap(find.text('发送'));
      await tester.pumpAndSettle();
      expect(c.pending.length, 1);
      expect(find.text('待发送'), findsOneWidget);
      await c.sync();
      await tester.pumpAndSettle();
      expect(c.pending, isEmpty);
      expect(c.items.any((i) => i['body'] == 'Android offline sample'), isTrue);

      final todo = c.items.firstWhere((i) => i['kind'] == 'todo');
      await c.submit('set_completed', item: todo, completed: true);
      await c.sync();
      expect(
        c.items.firstWhere((i) => i['id'] == todo['id'])['completed'],
        isTrue,
      );

      final text = c.items.firstWhere(
        (i) => i['body'] == 'Android offline sample',
      );
      await c.submit('update', item: text, body: 'local conflict draft');
      final remote = await http.post(
        Uri.parse('$server/api/v1/operations'),
        headers: {
          'Authorization': 'Bearer orbit-integration-test-token-00000002',
          'Content-Type': 'application/json',
        },
        body: jsonEncode({
          'operation_id': const Uuid().v4(),
          'item_id': text['id'],
          'expected_revision': text['revision'],
          'type': 'update',
          'body': 'edited on another device',
        }),
      );
      expect(remote.statusCode, 200);
      await c.sync();
      await tester.pumpAndSettle();
      expect(c.pending.single['state'], 'conflict');
      expect(c.pending.single['payload'], contains('local conflict draft'));
      await c.discard(c.pending.single);

      final dir = await getTemporaryDirectory();
      final file = File('${dir.path}/sample.png');
      await file.writeAsBytes(
        base64Decode(
          'iVBORw0KGgoAAAANSUhEUgAAABAAAAAMCAIAAADkharWAAAAFUlEQVR4nGPQKE8lCTGMahjVgB0BANOdwwFAryQoAAAAAElFTkSuQmCC',
        ),
      );
      await c.submit(
        'create',
        kind: 'image',
        body: 'sample image',
        photo: file.path,
      );
      await c.sync();
      expect(c.pending, isEmpty);
      final photo = c.items.firstWhere((i) => i['kind'] == 'image');
      expect(
        await (await c.image(photo['attachment_id'])).length(),
        greaterThan(0),
      );
      expect(
        await (await c.image(photo['attachment_id'], full: true)).length(),
        greaterThan(0),
      );
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      await file.delete();
      c.start();
    },
  );
}
