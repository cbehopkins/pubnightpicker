import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Form, Modal, Nav, Navbar, Table } from 'react-bootstrap';

const variants = ['A', 'B', 'C'];
const variantNames = { A: 'Two workspaces', B: 'Two-level navigation', C: 'Community first' };
const initialVenues = [
  { id: 'elm', name: 'The Elm Tree', location: 'Cambridge', type: 'Pub', notes: 'Quiet tables at the back.' },
  { id: 'blue', name: 'The Cambridge Blue', location: 'Cambridge', type: 'Pub', notes: 'Garden available.' },
  { id: 'mill', name: 'The Mill', location: 'Cambridge', type: 'Pub', notes: 'By the river.' },
  { id: 'eagle', name: 'The Eagle', location: 'Cambridge', type: 'Pub', notes: '' },
  { id: 'castle', name: 'The Castle Inn', location: 'Cambridge', type: 'Pub', notes: '' },
  { id: 'pint', name: 'The Pint Shop', location: 'Cambridge', type: 'Restaurant', notes: '' },
  { id: 'old', name: 'The Old Fountain', location: 'London', type: 'Pub', notes: '' },
  { id: 'harp', name: 'The Harp', location: 'London', type: 'Pub', notes: '' },
  { id: 'apple', name: 'The Apple', location: 'Bristol', type: 'Pub', notes: '' },
];
const initialLocations = [
  { id: 'cambridge', name: 'Cambridge', notes: 'Usual location for Cambridge Pub Night.' },
  { id: 'london', name: 'London', notes: 'Occasional away Meets.' },
  { id: 'bristol', name: 'Bristol', notes: '' },
];
const participation = ['This Week', 'Meets', 'Chat'];
const organising = ['Manage Meets', 'Venues', 'Locations'];

function Links({ items, view, go, vertical = false, responsive = false }) {
  return <Nav variant="pills" className={responsive ? 'flex-row flex-lg-column flex-wrap gap-1' : vertical ? 'flex-column gap-1' : 'gap-1 flex-wrap'}>
    {items.map(item => <Nav.Item key={item}><Nav.Link as="button" type="button" active={view === item} onClick={() => go(item)}>{item}</Nav.Link></Nav.Item>)}
  </Nav>;
}

function VariantA({ children, view, go, organiser, admin }) {
  const workspace = organising.includes(view) || view === 'New Meet';
  return <>
    <Navbar expand="md" bg="light" className="border-bottom px-3">
      <Navbar.Brand href="?variant=A">Pub Night Picker</Navbar.Brand>
      <Navbar.Toggle aria-controls="workspace-nav" />
      <Navbar.Collapse id="workspace-nav"><Links items={[...participation, ...(organiser ? ['Organise'] : []), ...(admin ? ['Admin'] : []), 'Account']} view={workspace ? 'Organise' : view} go={go} /></Navbar.Collapse>
    </Navbar>
    <div className="container-fluid py-4"><div className="row g-4">
      {workspace && <aside className="col-md-3 col-xl-2"><h2 className="h5">Organise</h2><Links items={organising} view={view} go={go} vertical /><Button variant="link" className="mt-3" onClick={() => go('This Week')}>Return to This Week</Button></aside>}
      <main className={workspace ? 'col-md-9 col-xl-10' : 'col-lg-10 offset-lg-1'}>{children}</main>
    </div></div>
  </>;
}

function VariantB({ children, view, go, organiser, admin }) {
  const places = ['Venues', 'Locations'].includes(view);
  const meets = ['Meets', 'Manage Meets', 'New Meet'].includes(view);
  return <>
    <header className="container-fluid border-bottom py-3"><div className="d-flex flex-wrap align-items-center justify-content-between gap-3">
      <a href="?variant=B" className="h4 text-decoration-none text-body mb-0">Pub Night Picker</a>
      <Links items={['This Week', 'Meets', 'Chat', ...(organiser ? ['Places'] : []), ...(admin ? ['Admin'] : []), 'Account']} view={places ? 'Places' : meets ? 'Meets' : view} go={go} />
    </div></header>
    {(places || meets) && <div className="bg-light border-bottom px-3 py-2"><Links items={places ? ['Venues', 'Locations'] : ['Meets', ...(organiser ? ['Manage Meets', 'New Meet'] : [])]} view={view} go={go} /></div>}
    <main className="container py-4">{children}</main>
  </>;
}

function VariantC({ children, view, go, organiser, admin }) {
  return <>
    <header className="border-bottom px-3 py-2 d-flex flex-wrap justify-content-between align-items-center gap-2">
      <a className="text-body text-decoration-none fw-semibold" href="?variant=C">Pub Night Picker</a>
      <Button variant="outline-secondary" onClick={() => go('Account')}>My Account</Button>
    </header>
    <div className="container-fluid"><div className="row">
      <aside className="col-lg-3 col-xl-2 bg-light border-end p-3">
        <img src="/app_logo.png" alt="" width="40" height="40" className="mb-2" />
        <h1 className="h5">Cambridge Pub Night</h1>
        <div className="small text-secondary mb-3">Cambridge community</div>
        <Links items={participation} view={view} go={go} responsive />
        {organiser && <Navbar expand="lg" className="d-block p-0 mt-3">
          <div className="d-flex justify-content-between align-items-center gap-2"><h2 className="h6 mb-0">Organise</h2><Navbar.Toggle aria-controls="community-tools" /></div>
          <Navbar.Collapse id="community-tools" className="mt-2"><Links items={[...organising, ...(admin ? ['Admin'] : [])]} view={view} go={go} vertical /></Navbar.Collapse>
        </Navbar>}
      </aside>
      <main className="col-lg-9 col-xl-10 p-4">{children}</main>
    </div></div>
  </>;
}

function PrototypeSwitcher({ variant, role, setRole, snapshot }) {
  const [, setParams] = useSearchParams();
  function cycle(direction) {
    const next = variants[(variants.indexOf(variant) + direction + variants.length) % variants.length];
    setParams(params => { params.set('variant', next); return params; }, { replace: true });
  }
  useEffect(() => {
    function onKey(event) {
      if (!(event.target instanceof HTMLElement) || event.target.closest('input, textarea, select, [contenteditable], [role="dialog"]') || event.altKey || event.ctrlKey || event.metaKey) return;
      if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
        event.preventDefault();
        cycle(event.key === 'ArrowRight' ? 1 : -1);
      }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });
  return <div className="position-fixed bottom-0 start-50 translate-middle-x bg-dark text-white border rounded p-2 mb-2 shadow" style={{ width: 'min(620px, calc(100% - 16px))', zIndex: 1040 }}>
    <div className="d-flex flex-wrap align-items-center justify-content-center gap-2">
      <span className="badge text-bg-warning">Prototype</span>
      <Button variant="outline-light" aria-label="Previous variant" title="Previous variant" onClick={() => cycle(-1)}>&larr;</Button>
      <span className="small text-center" style={{ width: 175 }}>{variant}: {variantNames[variant]}</span>
      <Button variant="outline-light" aria-label="Next variant" title="Next variant" onClick={() => cycle(1)}>&rarr;</Button>
      <Form.Select size="sm" aria-label="Prototype role" value={role} onChange={event => setRole(event.target.value)} style={{ width: 135 }}>
        <option>Participant</option><option>Organiser</option><option>Admin</option>
      </Form.Select>
    </div>
    <div className="small text-center mt-2 text-break" aria-live="polite">{snapshot}</div>
  </div>;
}

export default function Prototype() {
  const [params] = useSearchParams();
  const variant = variants.includes(params.get('variant')) ? params.get('variant') : 'A';
  const [role, setRole] = useState('Organiser');
  const [view, setView] = useState('This Week');
  const [venues, setVenues] = useState(initialVenues);
  const [locations, setLocations] = useState(initialLocations);
  const [search, setSearch] = useState('');
  const [editor, setEditor] = useState(null);
  const [voted, setVoted] = useState('');
  const [decided, setDecided] = useState(false);
  const [attendance, setAttendance] = useState('');
  const [draft, setDraft] = useState({ name: 'Thursday Pub Night', date: '2026-11-19', time: '19:30', venues: ['elm', 'blue', 'mill'] });
  const [createdMeets, setCreatedMeets] = useState([]);
  const [messages, setMessages] = useState([{ name: 'Alex', text: 'Looking forward to Thursday. Anyone coming straight from work?' }]);
  const [message, setMessage] = useState('');
  const [notice, setNotice] = useState('');
  const organiser = role !== 'Participant';
  const admin = role === 'Admin';
  function go(next) {
    setView(next === 'Organise' ? 'Manage Meets' : next === 'Places' ? 'Venues' : next);
    setSearch('');
    setNotice('');
  }
  function changeRole(next) {
    setRole(next);
    setEditor(null);
    if ((next === 'Participant' && (organising.includes(view) || view === 'New Meet')) || (next !== 'Admin' && view === 'Admin')) go('This Week');
  }
  function saveEditor(event) {
    event.preventDefault();
    if (editor.kind === 'venue') setVenues(current => current.map(venue => venue.id === editor.id ? { ...venue, name: editor.name, notes: editor.notes, location: editor.location, type: editor.type } : venue));
    else {
      const previous = locations.find(location => location.id === editor.id);
      setLocations(current => current.map(location => location.id === editor.id ? { ...location, name: editor.name, notes: editor.notes } : location));
      setVenues(current => current.map(venue => venue.location === previous.name ? { ...venue, location: editor.name } : venue));
    }
    setNotice(`${editor.name} saved.`);
    setEditor(null);
  }
  const chosenVenue = venues.find(venue => venue.id === (voted && voted !== 'any' ? voted : 'elm'));
  const candidates = venues.filter(venue => ['elm', 'blue', 'mill'].includes(venue.id));
  const Layout = variant === 'B' ? VariantB : variant === 'C' ? VariantC : VariantA;
  let content;
  if (view === 'This Week') content = <>
    <div className="d-flex flex-wrap align-items-start justify-content-between gap-3 mb-4"><div>
      <p className="text-secondary mb-1">Cambridge Pub Night</p><h1 className="h2">This Week</h1><p className="mb-0">Thursday 19 November &middot; 19:30</p>
    </div><span className={`badge ${decided ? 'text-bg-success' : 'text-bg-primary'}`}>{decided ? 'Venue chosen' : 'Voting open'}</span></div>
    {decided ? <section className="border-top border-bottom py-4">
      <h2 className="h3">{chosenVenue.name}</h2><p>{chosenVenue.location} &middot; {chosenVenue.notes}</p>
      <div className="d-flex flex-wrap gap-2"><Button variant={attendance === 'yes' ? 'success' : 'outline-success'} onClick={() => setAttendance('yes')}>I&apos;m coming</Button><Button variant={attendance === 'no' ? 'secondary' : 'outline-secondary'} onClick={() => setAttendance('no')}>Can&apos;t make it</Button></div>
      <p className="mt-3 mb-0" aria-live="polite">{attendance ? attendance === 'yes' ? 'You are coming. See you Thursday.' : 'You cannot make this Meet.' : '6 people coming'}</p>
    </section> : <section><h2 className="h4">Where shall we go?</h2>
      <div className="list-group mt-3">{candidates.map((venue, index) => <div className="list-group-item d-flex flex-wrap justify-content-between align-items-center gap-3" key={venue.id}>
        <div><h3 className="h5 mb-1">{venue.name}</h3><div className="small text-secondary">{venue.location} &middot; {index + 2 + (voted === venue.id ? 1 : 0)} votes</div></div>
        <Button variant={voted === venue.id ? 'success' : 'outline-primary'} onClick={() => setVoted(voted === venue.id ? '' : venue.id)}>{voted === venue.id ? 'Voted' : 'Vote'}</Button>
      </div>)}</div>
      <Button className="mt-3" variant={voted === 'any' ? 'success' : 'outline-secondary'} onClick={() => setVoted(voted === 'any' ? '' : 'any')}>Any venue works for me</Button>
      <p className="small text-secondary mt-3" aria-live="polite">{voted ? 'Your vote has been recorded.' : 'Voting closes Wednesday at 18:00.'}</p>
    </section>}
    <div className="d-flex flex-wrap gap-2 mt-4"><Button variant="outline-secondary" onClick={() => go('Chat')}>Meet Chat</Button><Button variant="link" onClick={() => go('Meets')}>Past Meets</Button></div>
    {organiser && <div className="border-top mt-4 pt-3 d-flex flex-wrap gap-2"><Button variant="outline-primary" onClick={() => go('New Meet')}>Create Meet</Button><Button variant="outline-secondary" onClick={() => setDecided(!decided)}>{decided ? 'Reopen voting' : 'Close voting'}</Button></div>}
  </>;
  else if (view === 'Meets' || view === 'Manage Meets') content = <>
    <div className="d-flex flex-wrap justify-content-between gap-2 mb-3"><h1 className="h2">{view}</h1>{organiser && <Button onClick={() => go('New Meet')}>Create Meet</Button>}</div>
    <h2 className="h5">Upcoming</h2><div className="list-group mb-4">
      <button className="list-group-item list-group-item-action" onClick={() => go('This Week')}><strong>Thursday Pub Night</strong><div>19 November &middot; {decided ? chosenVenue.name : 'Voting open'}</div></button>
      {createdMeets.map((meet, index) => <div className="list-group-item" key={index}><strong>{meet.name}</strong><div>{meet.date} &middot; {meet.time} &middot; {meet.venues.length} venues &middot; Voting open</div></div>)}
    </div><h2 className="h5">Past Meets</h2><div className="list-group">{['12 November - The Cambridge Blue', '5 November - The Mill', '29 October - The Elm Tree'].map(meet => <button key={meet} className="list-group-item list-group-item-action" onClick={() => setEditor({ kind: 'past', name: meet })}>{meet}</button>)}</div>
  </>;
  else if (view === 'New Meet') content = <>
    <h1 className="h2 mb-4">Create Meet</h1><Form onSubmit={event => { event.preventDefault(); setCreatedMeets(current => [...current, { ...draft, venues: [...draft.venues] }]); go('Manage Meets'); setNotice('Meet created.'); }}>
      <div className="row g-3 mb-4"><Form.Group className="col-12" controlId="meet-name"><Form.Label>Name</Form.Label><Form.Control required value={draft.name} onChange={event => setDraft({ ...draft, name: event.target.value })} /></Form.Group>
        <Form.Group className="col-sm-6" controlId="meet-date"><Form.Label>Date</Form.Label><Form.Control required type="date" value={draft.date} onChange={event => setDraft({ ...draft, date: event.target.value })} /></Form.Group>
        <Form.Group className="col-sm-6" controlId="meet-time"><Form.Label>Time</Form.Label><Form.Control required type="time" value={draft.time} onChange={event => setDraft({ ...draft, time: event.target.value })} /></Form.Group></div>
      <h2 className="h5">Venues</h2><Form.Control className="mb-3" aria-label="Search candidate venues" placeholder="Search venues" value={search} onChange={event => setSearch(event.target.value)} />
      <div className="list-group">{venues.filter(venue => venue.name.toLowerCase().includes(search.toLowerCase())).map(venue => <div key={venue.id} className="list-group-item d-flex flex-wrap align-items-center justify-content-between gap-2">
        <Form.Check id={`candidate-${venue.id}`} label={`${venue.name} (${venue.location})`} checked={draft.venues.includes(venue.id)} onChange={event => setDraft({ ...draft, venues: event.target.checked ? [...draft.venues, venue.id] : draft.venues.filter(id => id !== venue.id) })} />
        <Button variant="outline-secondary" size="sm" onClick={() => setEditor({ ...venue, kind: 'venue' })}>Edit</Button>
      </div>)}</div><div className="d-flex gap-2 mt-4"><Button type="submit" disabled={!draft.venues.length}>Create Meet</Button><Button variant="outline-secondary" onClick={() => go('Manage Meets')}>Cancel</Button></div>
    </Form>
  </>;
  else if (view === 'Venues') content = <>
    <h1 className="h2 mb-3">Venues</h1><Form.Control aria-label="Search venues" placeholder="Search venues" value={search} onChange={event => setSearch(event.target.value)} className="mb-3" />
    <div className="table-responsive"><Table striped hover><thead><tr><th>Name</th><th>Location</th><th>Type</th><th><span className="visually-hidden">Actions</span></th></tr></thead><tbody>
      {venues.filter(venue => `${venue.name} ${venue.location}`.toLowerCase().includes(search.toLowerCase())).map(venue => <tr key={venue.id}><td>{venue.name}</td><td>{venue.location}</td><td>{venue.type}</td><td><Button size="sm" variant="outline-primary" onClick={() => setEditor({ ...venue, kind: 'venue' })}>Edit</Button></td></tr>)}
    </tbody></Table></div>
  </>;
  else if (view === 'Locations') content = <>
    <h1 className="h2 mb-3">Locations</h1><div className="list-group">{locations.map(location => <div key={location.id} className="list-group-item py-3 d-flex flex-wrap justify-content-between align-items-center gap-2">
      <div><h2 className="h5 mb-1">{location.name}</h2><span className="text-secondary">{venues.filter(venue => venue.location === location.name).length} venues</span></div>
      <Button variant="outline-primary" onClick={() => setEditor({ ...location, kind: 'location' })}>Edit</Button>
    </div>)}</div>
  </>;
  else if (view === 'Chat') content = <>
    <h1 className="h2 mb-4">Meet Chat</h1><div className="list-group mb-3">{messages.map((item, index) => <div className="list-group-item text-break" key={index}><strong>{item.name}</strong><p className="mb-0">{item.text}</p></div>)}</div>
    <Form onSubmit={event => { event.preventDefault(); if (!message.trim()) return; setMessages(current => [...current, { name: 'You', text: message.trim() }]); setMessage(''); }}><Form.Group controlId="chat-message"><Form.Label>Message</Form.Label><Form.Control as="textarea" rows={3} required value={message} onChange={event => setMessage(event.target.value)} /></Form.Group><Button type="submit" className="mt-2">Send</Button></Form>
  </>;
  else if (view === 'Admin') content = <>
    <h1 className="h2">System Administration</h1><div className="list-group mt-3">{['Users and permissions', 'Configuration', 'Diagnostics', 'Logs'].map(item => <button key={item} className="list-group-item list-group-item-action" onClick={() => setEditor({ kind: 'admin', name: item })}>{item}</button>)}</div>
  </>;
  else content = <>
    <h1 className="h2 mb-4">My Account</h1><Form onSubmit={event => { event.preventDefault(); setNotice('Preferences saved.'); }}>
      <Form.Group controlId="account-name" className="mb-3"><Form.Label>Display name</Form.Label><Form.Control defaultValue="Chris" required /></Form.Group>
      <Form.Check id="email-preference" type="switch" label="Email Meet reminders" defaultChecked className="mb-3" /><Button type="submit">Save Preferences</Button>
    </Form>
  </>;
  return <div style={{ paddingBottom: 180 }}>
    <Layout view={view} go={go} organiser={organiser} admin={admin}>
      {notice && <div role="status" className="alert alert-success">{notice}</div>}{content}
    </Layout>
    <Modal show={!!editor} onHide={() => setEditor(null)} centered scrollable>
      <Modal.Header closeButton><Modal.Title className="h5">{editor?.name}</Modal.Title></Modal.Header>
      {editor && ['venue', 'location'].includes(editor.kind) ? <Form onSubmit={saveEditor}><Modal.Body>
        <Form.Group controlId="edit-name" className="mb-3"><Form.Label>Name</Form.Label><Form.Control required value={editor.name} onChange={event => setEditor({ ...editor, name: event.target.value })} /></Form.Group>
        {editor.kind === 'venue' && <><Form.Group controlId="edit-location" className="mb-3"><Form.Label>Location</Form.Label><Form.Select value={editor.location} onChange={event => setEditor({ ...editor, location: event.target.value })}>{locations.map(location => <option key={location.id}>{location.name}</option>)}</Form.Select></Form.Group>
          <Form.Group controlId="edit-type" className="mb-3"><Form.Label>Type</Form.Label><Form.Select value={editor.type} onChange={event => setEditor({ ...editor, type: event.target.value })}><option>Pub</option><option>Restaurant</option><option>Event</option></Form.Select></Form.Group></>}
        <Form.Group controlId="edit-notes"><Form.Label>Notes</Form.Label><Form.Control as="textarea" rows={3} value={editor.notes} onChange={event => setEditor({ ...editor, notes: event.target.value })} /></Form.Group>
        {editor.kind === 'location' && <div className="mt-3"><h3 className="h6">Venues</h3><ul>{venues.filter(venue => venue.location === locations.find(location => location.id === editor.id)?.name).map(venue => <li key={venue.id}>{venue.name}</li>)}</ul></div>}
      </Modal.Body><Modal.Footer><Button variant="secondary" onClick={() => setEditor(null)}>Cancel</Button><Button type="submit">Save</Button></Modal.Footer></Form> : <>
        <Modal.Body>{editor?.kind === 'past' ? <><p>Meet completed &middot; 7 people attended</p><Button variant="outline-secondary" onClick={() => { setEditor(null); go('Chat'); }}>Meet Chat</Button></> : <p className="mb-0">{editor?.name === 'Users and permissions' ? 'Chris - Administrator; Alex - Organiser; Sam - Participant' : editor?.name === 'Diagnostics' ? 'Services healthy' : editor?.name === 'Logs' ? '19:30 - Meet notification queued' : 'Default meeting time: 19:30'}</p>}</Modal.Body>
        <Modal.Footer><Button variant="secondary" onClick={() => setEditor(null)}>Close</Button></Modal.Footer>
      </>}
    </Modal>
    <PrototypeSwitcher variant={variant} role={role} setRole={changeRole} snapshot={`${view} | ${role} | ${decided ? 'Decided' : 'Voting'} | Vote: ${voted || 'none'} | Attendance: ${attendance || 'unset'} | Draft: ${draft.venues.length} venues | Created: ${createdMeets.length} | Edited: ${venues.filter((venue, index) => JSON.stringify(venue) !== JSON.stringify(initialVenues[index])).length} venues`} />
  </div>;
}
