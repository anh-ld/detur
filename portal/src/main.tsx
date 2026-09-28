import { render } from 'preact';
import 'kinu/style.css';
import './theme.css';
import { App } from './app';

render(<App />, document.getElementById('app')!);