import 'package:flutter/material.dart';
import 'package:orbit_android/main.dart' show NodePage;
import 'inbox_screen.dart' show InboxReadingNotification;

/// One Android application for the shared inbox and existing desktop widgets.
class OrbitHome extends StatefulWidget {
  final Widget inbox;
  final bool widgetsEnabled;
  const OrbitHome({
    super.key,
    required this.inbox,
    required this.widgetsEnabled,
  });

  @override
  State<OrbitHome> createState() => _OrbitHomeState();
}

class _OrbitHomeState extends State<OrbitHome> {
  int selected = 0;
  bool reading = false;

  @override
  Widget build(BuildContext context) {
    if (!widget.widgetsEnabled) return widget.inbox;
    return Scaffold(
      body: NotificationListener<InboxReadingNotification>(
        onNotification: (notification) {
          if (selected == 0 && reading != notification.reading) {
            setState(() => reading = notification.reading);
          }
          return true;
        },
        child: IndexedStack(
          index: selected,
          children: [widget.inbox, const NodePage()],
        ),
      ),
      bottomNavigationBar: AnimatedSize(
        duration: const Duration(milliseconds: 200),
        alignment: Alignment.bottomCenter,
        child: reading && selected == 0
            ? const SizedBox.shrink()
            : NavigationBar(
                selectedIndex: selected,
                onDestinationSelected: (value) => setState(() {
                  selected = value;
                  reading = false;
                }),
                destinations: const [
                  NavigationDestination(
                    icon: Icon(Icons.inbox_outlined),
                    selectedIcon: Icon(Icons.inbox),
                    label: '收件箱',
                  ),
                  NavigationDestination(
                    icon: Icon(Icons.widgets_outlined),
                    selectedIcon: Icon(Icons.widgets),
                    label: '状态与组件',
                  ),
                ],
              ),
      ),
    );
  }
}
