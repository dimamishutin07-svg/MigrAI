'use strict';
const $ = id => document.getElementById(id);
const stages = {survey:'Заполняет анкету',review:'Проверяет портрет',ready:'Портрет готов',chat:'Обсуждает маршруты'};
const kinds = {user:'Пользователь',assistant:'MigrAI',rating:'Оценка',feedback:'Отзыв',feedback_request:'Запрос отзыва',action:'Действие'};
let offset=0, total=0, fields=[], selected=null, eventLimit=100, busy=false, queryVersion=0;
const date = value => value ? new Intl.DateTimeFormat('ru-RU',{dateStyle:'short',timeStyle:'short',timeZone:'Europe/Moscow'}).format(new Date(value)) : '—';
function node(tag,text,cls){const n=document.createElement(tag);if(text!=null)n.textContent=text;if(cls)n.className=cls;return n;}
function showLogin(){ $('login').hidden=false;$('app').hidden=true;if($('detail').open)$('detail').close(); }
async function api(path,options){const r=await fetch(path,options);if(r.status===401){showLogin();throw Error('Войдите в панель');}if(!r.ok)throw Error(await r.text());return r.status===204?null:r.json();}
function parameters(){return new URLSearchParams({q:$('search').value.trim().replace(/^@/,''),stage:$('stage').value});}
function renderPeople(items){
 $('people').replaceChildren();$('empty').hidden=items.length>0;
 for(const p of items){const tr=node('tr');const user=node('td');const link=node('button',p.name||p.username||'Пользователь '+p.id,'person');link.onclick=()=>openPerson(p.id);user.append(link,node('small',p.username?'@'+p.username:'ID '+p.id));
 const plans=node('td');plans.append(node('strong',p.profile.countries||'Страны пока не выбраны'),node('small',p.profile.goal||'Цель пока не указана'));
 const stage=node('td');stage.append(node('span',stages[p.stage],'badge '+p.stage));let filled=fields.filter(f=>p.profile[f.key]&&p.profile[f.key]!=='не указано').length;stage.append(node('small','Заполнено полей: '+filled));
 const conversation=node('td');conversation.append(node('p',p.last_question||'Пока нет сообщений','snippet'));if(p.last_answer)conversation.append(node('small','Бот: '+p.last_answer,'snippet'));
 const activity=node('td',date(p.last_seen));activity.append(node('small',p.message_count+' сообщений за 90 дней'));
 tr.append(user,plans,stage,conversation,activity,node('td',p.feedback||'Пока без отзыва'));$('people').append(tr);
 }
 $('total').textContent=total;$('prev').disabled=offset===0;$('next').disabled=offset+50>=total;$('page').textContent=total?`${offset+1}–${Math.min(offset+50,total)} из ${total}`:'0 пользователей';
}
async function refresh(){
 if(busy||$('app').hidden)return;busy=true;const version=queryVersion;
 try{const params=parameters();params.set('offset',offset);const data=await api('/api/users?'+params);if(version!==queryVersion)return;total=data.total;if(offset>=total&&offset>0){offset=Math.max(0,Math.floor((total-1)/50)*50);queryVersion++;return;}renderPeople(data.items);$('error').textContent='';$('sync').textContent='Обновлено '+new Intl.DateTimeFormat('ru-RU',{timeStyle:'medium',timeZone:'Europe/Moscow'}).format(new Date());if(selected&&$('detail').open)await refreshPerson();}
 catch(e){$('error').textContent=e.message;$('sync').textContent='Обновление не удалось';}finally{busy=false;if(version!==queryVersion)refresh();}
}
function profile(p){
 $('detail-name').textContent=p.name||p.username||'Пользователь '+p.id;
 const identity=node('div',null,'identity');identity.append(node('span','Telegram ID: '+p.id),node('span','Чат: '+(p.chat_id||'появится при новом сообщении')),node('span','Телефон: '+(p.phone||'не предоставлен')),node('span','Связь с командой: '+(p.contact_consent?'согласие получено':'согласие не получено')),node('span','Первое обращение: '+date(p.first_seen)),node('span','Последняя активность: '+date(p.last_seen)));
 if(p.username&&/^[a-zA-Z0-9_]+$/.test(p.username)){const link=node('a','Открыть @'+p.username);link.href='https://t.me/'+p.username;link.target='_blank';link.rel='noopener noreferrer';identity.append(link);}
 $('identity').replaceChildren(identity);$('profile').replaceChildren();let section='',list;
 for(const f of fields){if(section!==f.section){section=f.section;$('profile').append(node('h3',section,'profile-section'));list=node('dl');$('profile').append(list);}list.append(node('dt',f.label),node('dd',p.profile[f.key]||'Не указано'));}
 const q=fields.find(f=>'survey:'+f.key===p.question);if(q)$('identity').append(node('p','Сейчас отвечает на вопрос «'+q.label+'».','footnote'));
}
async function refreshPerson(){
 const id=selected;
 try{const params=new URLSearchParams({q:'id:'+id});const data=await api('/api/users?'+params);if(id!==selected)return;const person=data.items.find(p=>p.id===id);if(!person){$('detail-error').textContent='Пользователь удалил профиль или больше недоступен.';return;}profile(person);const events=[];for(let start=0;start<eventLimit;start+=100){const page=await api('/api/events?'+new URLSearchParams({id,offset:start}));events.push(...page);if(page.length<100)break;}
 if(id!==selected)return;$('events').replaceChildren();for(const e of events){const item=node('article',null,'event');const head=node('header');head.append(node('strong',kinds[e.kind]||e.kind),node('time',date(e.at)));item.append(head,node('p',e.text));$('events').append(item);}if(!events.length)$('events').append(node('p','Истории за последние 90 дней пока нет.'));$('more-events').hidden=events.length<eventLimit;$('detail-error').textContent='';
 }catch(e){$('detail-error').textContent=e.message;}
}
async function openPerson(id){selected=id;eventLimit=100;$('detail-error').textContent='';$('profile').replaceChildren();$('identity').replaceChildren();$('events').replaceChildren();$('detail-name').textContent='Загрузка…';setTab(false);$('detail').showModal();await refreshPerson();}
function setTab(history){$('profile').hidden=history;$('history').hidden=!history;$('profile-tab').setAttribute('aria-selected',!history);$('history-tab').setAttribute('aria-selected',history);}
async function start(){try{fields=await api('/api/fields');$('login').hidden=true;$('app').hidden=false;await refresh();}catch(e){if(e.message!=='Войдите в панель')$('login-error').textContent=e.message;}}
$('login-form').onsubmit=async e=>{e.preventDefault();const button=e.target.querySelector('button');button.disabled=true;$('login-error').textContent='';try{await api('/login',{method:'POST',body:new URLSearchParams({password:$('password').value})});$('password').value='';await start();}catch(e){$('login-error').textContent=e.message;}finally{button.disabled=false;}};
$('logout').onclick=async()=>{await api('/logout',{method:'POST'});showLogin();};
$('close-detail').onclick=()=>{$('detail').close();selected=null;};$('detail').onclose=()=>{selected=null;};
$('profile-tab').onclick=()=>setTab(false);$('history-tab').onclick=()=>setTab(true);
$('more-events').onclick=async()=>{eventLimit+=100;await refreshPerson();};
$('prev').onclick=()=>{offset=Math.max(0,offset-50);queryVersion++;refresh();};$('next').onclick=()=>{offset+=50;queryVersion++;refresh();};
let timer;function change(){offset=0;queryVersion++;clearTimeout(timer);timer=setTimeout(refresh,250);} $('search').oninput=change;$('stage').onchange=change;
$('export').onclick=async()=>{const button=$('export');button.disabled=true;try{const r=await fetch('/export.xlsx?'+parameters());if(r.status===401){showLogin();throw Error('Войдите снова');}if(!r.ok)throw Error(await r.text());const url=URL.createObjectURL(await r.blob());const a=node('a');a.href=url;a.download='migrai-users.xlsx';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}catch(e){$('error').textContent=e.message;}finally{button.disabled=false;}};
setInterval(()=>{if(!document.hidden)refresh();},8000);start();
