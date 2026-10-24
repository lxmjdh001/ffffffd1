import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Root } from './root';
import './styles.css';

if (!window.location.hash) window.history.replaceState(null, '', `/${window.location.search}#/welcome`);
createRoot(document.getElementById('root')!).render(<StrictMode><Root /></StrictMode>);
