import { Component, type ReactNode } from "react";

type State = { error: Error | null };

export class GraphQLErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  render() {
    if (this.state.error) {
      return (
        <section className="panel" role="alert">
          <p>購入履歴を取得できませんでした</p>
          <button className="button button-soft" onClick={() => window.location.reload()}>
            再読み込み
          </button>
        </section>
      );
    }
    return this.props.children;
  }
}
