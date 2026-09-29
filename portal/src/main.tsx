import { Component, ComponentChildren, render } from 'preact';
import 'kinu/style.css';
import './theme.css';
import { Button } from 'kinu';
import { App } from './app';
import { muted } from './ui';

class ErrorBoundary extends Component<{ children: ComponentChildren }, { err: Error | null }> {
  state = { err: null as Error | null };

  componentDidCatch(err: Error) {
    console.error('portal crash:', err);
    this.setState({ err });
  }

  render() {
    if (this.state.err) {
      return (
        <main style={{ maxWidth: 640, margin: '48px auto', padding: '0 24px', textAlign: 'center' }}>
          <h1 style={{ margin: '0 0 8px' }}>Something went wrong</h1>
          <p style={muted}>The portal hit an unexpected error. Reload to get back to the apps list.</p>
          <Button
            onClick={() => {
              location.hash = '#/';
              location.reload();
            }}
          >
            Back to apps
          </Button>
        </main>
      );
    }
    return this.props.children;
  }
}

render(
  <ErrorBoundary>
    <App />
  </ErrorBoundary>,
  document.getElementById('app')!,
);