const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function fixture(fetcher) {
  const listeners = {};
  const elements = {};
  const players = [];
  const element = () => ({hidden:false, textContent:'', style:{}, dataset:{}, events:{},
    setAttribute(k,v){this[k]=v;}, removeAttribute(k){delete this[k];},
    replaceChildren(...children){this.children=children;}, addEventListener(n,f){this.events[n]=f;},
    pause(){this.paused=true;}, load(){this.released=true;}});
  for(const id of ['media-player','media-player-title','media-player-status','media-player-error','media-player-download','media-player-content']) elements[id] = element();
  const dialog = elements['media-player']; dialog.id='media-player';
  dialog.showModal=()=>{dialog.open=true;};
  dialog.close=()=>{dialog.open=false;listeners.close({target:dialog});};
  const document = {getElementById(id){return elements[id];}, addEventListener(n,f){listeners[n]=f;},createElement(kind){const p=element();p.kind=kind;players.push(p);return p;}};
  vm.runInNewContext(fs.readFileSync(__dirname+'/media-player.js','utf8'),{document,fetch:fetcher,AbortController});
  const button = {dataset:{mediaUrl:'/media/test'},isConnected:true,focus(){this.focused=true;}};
  return {elements,players,listeners,dialog,button,click(){listeners.click({target:{closest(s){return s==='[data-media-url]'?button:null;}},preventDefault(){}});}};
}
const metadata = {name:'A movie.mp4',kind:'video',mime:'video/mp4',streamURL:'/media/test/stream',downloadURL:'https://store1.gofile.io/download/web/test/A%20movie.mp4'};
const flush = () => new Promise(resolve=>setImmediate(resolve));

test('opens native video with controls and fallback, no autoplay', async()=>{
 const f=fixture(async()=>({ok:true,json:async()=>metadata})); f.click();
 assert.equal(f.dialog.open,true); await flush();
 const p=f.players[0]; assert.equal(p.kind,'video');assert.equal(p.controls,true);assert.equal(p.autoplay,undefined);assert.equal(p.src,metadata.streamURL);
 assert.equal(f.elements['media-player-title'].textContent,metadata.name);
 assert.equal(f.elements['media-player-download'].href,metadata.downloadURL);
 p.events.loadedmetadata();assert.equal(f.elements['media-player-status'].hidden,true);
 p.events.error();assert.equal(f.elements['media-player-error'].hidden,false);
 f.dialog.close(); assert.ok(p.paused);assert.ok(p.released);assert.equal(p.src,undefined);assert.ok(f.button.focused);
});

test('audio uses audio element',async()=>{
 const f=fixture(async()=>({ok:true,json:async()=>({...metadata,kind:'audio'})}));f.click();await flush();assert.equal(f.players[0].kind,'audio');
});

test('access failure shows contextual error without constructing player',async()=>{
 const f=fixture(async()=>({ok:false,status:404}));f.click();await flush();assert.equal(f.players.length,0);assert.match(f.elements['media-player-error'].textContent,/do not have access/);
});

test('closing pending lookup aborts and rejects late result',async()=>{
 let resolve, signal;
 const f=fixture((url,options)=>{signal=options.signal;return new Promise(r=>{resolve=r;});});f.click();f.dialog.close();assert.ok(signal.aborted);
 resolve({ok:true,json:async()=>metadata});await flush();assert.equal(f.players.length,0);
});

test('library refresh preserves playback but body swap releases it',async()=>{
 const f=fixture(async()=>({ok:true,json:async()=>metadata}));f.click();await flush();const p=f.players[0];
 f.listeners['htmx:beforeSwap']({detail:{target:{contains(){return false;}}}});assert.equal(p.paused,undefined);
 f.listeners['htmx:beforeSwap']({detail:{target:{contains(){return true;}}}});assert.equal(p.paused,true);
});
