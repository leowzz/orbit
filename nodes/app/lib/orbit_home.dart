import 'package:flutter/material.dart';
import 'package:orbit_android/main.dart' show NodePage;

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

  @override
  Widget build(BuildContext context) {
    if (!widget.widgetsEnabled) return widget.inbox;
    return Scaffold(
      body: IndexedStack(
        index: selected,
        children: [widget.inbox, const NodePage()],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: selected,
        onDestinationSelected: (value) => setState(() => selected = value),
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
    );
  }
}
