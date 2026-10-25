import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Root } from './root';
import './styles.css';

if (!window.location.hash) window.history.replaceState(null, '', `/${window.location.search}#/welcome`);
const rootElement = document.getElementById('root')!;
const initialHomeHTML = rootElement.innerHTML;
createRoot(rootElement).render(<StrictMode><Root initialHomeHTML={initialHomeHTML} /></StrictMode>);
