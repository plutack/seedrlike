const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
function fixture() {
 const listeners={}; let aborted=false;
 const modal={open:false,showModal(){this.open=true},close(){this.open=false}};
 const content={replaceChildren(){}};
 const document={addEventListener(name,fn){listeners[name]=fn},getElementById(id){return {'file-picker':modal,'file-picker-content':content,'file-picker-loading':{content:{cloneNode(){return {}}}}}[id]}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/download-ui.js','utf8'),{document,window:{}});
 return {listeners,modal,xhr:{abort(){aborted=true}},get aborted(){return aborted}};
}
function upload() {
 const window={};vm.runInNewContext(fs.readFileSync(__dirname+'/download-ui.js','utf8'),{window,document:{addEventListener(){}}});
 return Object.assign(window.torrentUpload(),{$refs:{file:{value:'',focus(){}}},$root:{querySelector(){return {replaceChildren(){}}}}});
}
test('upload validates file extension count size and emptiness',()=>{
 const u=upload();for(const files of [[{name:'a.txt',size:1}],[{name:'a.torrent',size:0}],[{name:'a.torrent',size:4194305}],[{name:'a.torrent',size:1},{name:'b.torrent',size:1}]]){u.choose(files);assert.equal(u.file,null);assert.ok(u.error)}
 u.choose([{name:'GOOD.TORRENT',size:100}]);assert.equal(u.file.name,'GOOD.TORRENT');assert.equal(u.error,'');
});
test('upload blocks duplicate requests and edits; error retains file; acceptance clears file',()=>{
 const u=upload();u.choose([{name:'a.torrent',size:100}]);u.start({preventDefault(){assert.fail('first submit prevented')}});assert.equal(u.busy,true);
 let prevented=false;u.start({preventDefault(){prevented=true}});assert.ok(prevented);u.remove();assert.ok(u.file);u.progress({detail:{loaded:50,total:100}});assert.equal(u.percent,50);
 u.finish({detail:{failed:true}});assert.ok(u.error);assert.ok(u.file);assert.equal(u.busy,false);u.accepted();assert.equal(u.file,null);assert.equal(u.error,'');
});
test('preview immediately opens modal; closing aborts lookup',()=>{
 const f=fixture();f.listeners['htmx:beforeRequest']({detail:{target:{id:'file-picker-content'},requestConfig:{path:'/downloads/preview'},xhr:f.xhr}});
 assert.equal(f.modal.open,true);f.listeners.close({target:{id:'file-picker'}});assert.equal(f.aborted,true);
});
test('multipart preview opens for array or missing action parameters',()=>{
 for(const parameters of [{action:['preview']},{},undefined]){
  const f=fixture();f.listeners['htmx:beforeRequest']({detail:{target:{id:'file-picker-content'},requestConfig:{path:'/downloads/torrent',parameters},xhr:f.xhr}});assert.equal(f.modal.open,true);
  const u=upload();u.choose([{name:'a.torrent',size:100}]);u.start({detail:{target:{id:'file-picker-content'}},preventDefault(){}});assert.equal(u.previewing,true);
 }
});
test('ordinary upload does not open the picker',()=>{const f=fixture();f.listeners['htmx:beforeRequest']({detail:{target:{id:'torrent-upload-response'},requestConfig:{path:'/downloads/torrent'},xhr:f.xhr}});assert.equal(f.modal.open,false)});
test('queue success closes modal',()=>{const f=fixture();f.modal.open=true;f.listeners['download-queued']();assert.equal(f.modal.open,false)});
