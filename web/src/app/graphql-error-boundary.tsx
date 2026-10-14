import { Component, Fragment, type ReactNode } from "react";

type State = { error: Error | null; retryKey: number };

export class GraphQLErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null, retryKey: 0 };

  static getDerivedStateFromError(error: Error): State {
    return { error, retryKey: 0 };
  }

  render() {
    if (this.state.error) {
      return (
        <section className="panel" role="alert">
          <p>データを取得できませんでした</p>
          <button
            className="button button-soft"
            type="button"
            onClick={() =>
              this.setState((state) => ({ error: null, retryKey: state.retryKey + 1 }))
            }
          >
            再試行
          </button>
        </section>
      );
    }
    return <Fragment key={this.state.retryKey}>{this.props.children}</Fragment>;
  }
}
