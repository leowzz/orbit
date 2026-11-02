import { Component } from "react";
import type { ReactNode } from "react";

export default class PageBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? (
      <div className="connection-error" role="alert">
        <h1>页面未能加载</h1>
        <p>请检查连接后重新载入。已保存的收件箱草稿会保留。</p>
        <button className="button" onClick={() => window.location.reload()}>
          重新载入
        </button>
      </div>
    ) : (
      this.props.children
    );
  }
}
