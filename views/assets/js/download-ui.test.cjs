const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
function fixture() {
 const listeners={}; let aborted=false;
 const modal={open:false,showModal(){this.open=true},close(){this.open=false}};
 const content={replaceChildren(){}};
 const document={addEventListener(name,fn){listeners[name]=fn},getElementById(id){return {'file-picker':modal,'file-picker-content':content,'file-picker-loading':{content:{cloneNode(){return {}}}}}[id]}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/download-ui.js','utf8'),{document});
 return {listeners,modal,xhr:{abort(){aborted=true}},get aborted(){return aborted}};
}
test('preview immediately opens modal; closing aborts lookup',()=>{
 const f=fixture();f.listeners['htmx:beforeRequest']({detail:{requestConfig:{path:'/downloads/preview'},xhr:f.xhr}});
 assert.equal(f.modal.open,true);f.listeners.close({target:{id:'file-picker'}});assert.equal(f.aborted,true);
});
test('queue success closes modal',()=>{const f=fixture();f.modal.open=true;f.listeners['download-queued']();assert.equal(f.modal.open,false)});
