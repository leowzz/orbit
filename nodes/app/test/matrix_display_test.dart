import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/matrix_display.dart';
import 'package:orbit_app/local_store.dart';

void main() {
  final now = DateTime.utc(2026, 10, 15, 8);
  Json view({double? remaining = 72.5, DateTime? reset, DateTime? fresh}) => {
    'codex': {
      'freshUntil': now.add(const Duration(hours: 1)).toIso8601String(),
      'weeklyLimit': {
        'remainingPercent': ?remaining,
        'resetsAt':
            (reset ?? now.add(const Duration(days: 4, hours: 2, minutes: 31)))
                .toIso8601String(),
        'observedAt': now.toIso8601String(),
        'freshUntil': (fresh ?? now.add(const Duration(minutes: 3)))
            .toIso8601String(),
      },
    },
  };

  test('API page follows the route even without a quota observation', () {
    final reading = MatrixReading.fromView(
      {
        'displayKind': 'usage',
        'usage': {
          'actualCostMicros': '12345000',
          'tokenCount': '1200000',
          'tpm': '500',
          'freshUntil': now.add(const Duration(minutes: 1)).toIso8601String(),
        },
      },
      true,
      now,
    );
    expect(reading.showUsage, isTrue);
    expect(reading.costText, '12.35');
    expect(reading.tokensText, '1.2M');
    expect(reading.status, '实时');
    expect(
      MatrixReading.fromView({'displayKind': 'usage'}, true, now).status,
      '暂无数据',
    );
  });
  test('weekly percentage, consumption and whole-minute countdown', () {
    final reading = MatrixReading.fromView(view(), true, now);
    expect(reading.percent, '72.5%');
    expect(reading.used, '已用 27.5%');
    expect(reading.countdown, '4天 2时 31分');
    expect(reading.status, '实时');
    expect(reading.displayPercent, '73%');
    expect(reading.displayCountdown, '4D 02H');
    expect(reading.displayStatus, '');
  });
  test('proto3 omitted zero is exhausted, absent quota is unavailable', () {
    expect(
      MatrixReading.fromView(view(remaining: null), true, now).percent,
      '0%',
    );
    expect(MatrixReading.fromView({}, true, now).remaining, isNull);
    expect(MatrixReading.fromView(null, false, now).status, '暂无周额度');
    expect(MatrixReading.fromView(null, false, now).displayStatus, 'NO DATA');
  });
  test(
    'session freshness cannot make stale quota fresh, offline is explicit',
    () {
      final stale = view(fresh: now.subtract(const Duration(seconds: 1)));
      expect(MatrixReading.fromView(stale, true, now).status, '上次数据');
      expect(MatrixReading.fromView(stale, false, now).status, '离线缓存');
      expect(MatrixReading.fromView(stale, true, now).percent, '72.5%');
    },
  );
  test(
    'passed reset hides both quota percentages without claiming renewal',
    () {
      final reading = MatrixReading.fromView(view(reset: now), true, now);
      expect(reading.remaining, isNull);
      expect(reading.used, '已用 —');
      expect(reading.status, '等待重置更新');
      expect(reading.countdown, '等待更新');
      expect(reading.displayPercent, '--%');
      expect(reading.displayCountdown, '--D --H');
      expect(reading.displayStatus, 'RESETTING');
    },
  );
  test('invalid values and restored settings stay bounded', () {
    for (final value in [-1.0, 101.0, double.nan, double.infinity]) {
      expect(
        MatrixReading.fromView(view(remaining: value), true, now).remaining,
        isNull,
      );
    }
    final style = MatrixStyle.fromJson({
      'density': 1,
      'glowRadius': 100,
      'glowStrength': 'NaN',
    });
    expect(style.density, 64);
    expect(style.glowRadius, 2.5);
    expect(style.glowStrength, .65);
    final disabled = MatrixStyle.fromJson(
      const MatrixStyle(glowRadius: 0).toJson(),
    );
    expect(disabled.glowRadius, 0);
  });
}
