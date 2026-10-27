import { mount } from 'svelte';
import '../../../../shared/theme/warm.css';
import './dashboard.css';
import App from './App.svelte';
import MonitorWindow from './MonitorWindow.svelte';
import ReviewWindow from './ReviewWindow.svelte';
import ImageWindow from './ImageWindow.svelte';
import { route } from './route.js';

const { page, props } = route(location.hash);
// Go reuses a window by pointing it at a new address; a changed address means a different student or picture.
if (page !== 'main') window.addEventListener('hashchange', () => location.reload());
mount({ main: App, monitoring: MonitorWindow, review: ReviewWindow, image: ImageWindow }[page], { target: document.getElementById('app'), props });
