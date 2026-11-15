import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import 'bootstrap/dist/css/bootstrap.min.css';
import Prototype from './Prototype';

document.title = 'Navigation Prototype - Pub Night Picker';
createRoot(document.getElementById('root')).render(<BrowserRouter><Prototype /></BrowserRouter>);