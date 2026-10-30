import { Button } from "@/components/ui/button";
import { Component, type ReactNode } from "react";

type Props = {
  children: ReactNode;
  message?: string;
  fallback?: ReactNode;
};

export class LoadBoundary extends Component<Props, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (!this.state.failed) return this.props.children;
    if (this.props.fallback !== undefined) return this.props.fallback;
    // React.lazy caches rejected imports; resetting only the boundary would
    // throw again. A user-requested reload also clears the browser module map.
    return (
      <div role="alert" className="space-y-3 p-6">
        <p>{this.props.message || "页面加载失败，请重新加载页面。"}</p>
        <Button variant="outline" onClick={() => window.location.reload()}>
          重新加载页面
        </Button>
      </div>
    );
  }
}
