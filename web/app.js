const $ = id => document.getElementById(id);
function cell(value, className='') { const el=document.createElement('td'); el.textContent=value; el.className=className; return el; }
function render(jobs) {
  $('total').textContent=jobs.length;
  for(const status of ['pending','delivered','dead']) $(status).textContent=jobs.filter(j=>j.status===status).length;
  const body=$('jobs'); body.replaceChildren();
  if(!jobs.length){const row=document.createElement('tr');const td=cell('Nenhum evento ainda. Use a API assinada para enviar o primeiro.','empty');td.colSpan=5;row.append(td);body.append(row);}
  for(const job of jobs.slice(0,100)){
    const row=document.createElement('tr');const name=document.createElement('td');const strong=document.createElement('strong');strong.textContent=job.kind;const small=document.createElement('small');small.textContent=job.id;name.append(strong,small);row.append(name);
    const status=document.createElement('td');const badge=document.createElement('span');badge.className='badge '+job.status;badge.textContent=({pending:'EM FILA',delivered:'ENTREGUE',dead:'DEAD LETTER'})[job.status]||job.status;status.append(badge);row.append(status);
    row.append(cell(job.attempts));row.append(cell(new Intl.DateTimeFormat('pt-BR',{dateStyle:'short',timeStyle:'short'}).format(new Date(job.createdAt))));row.append(cell(job.lastError||'—','error'));body.append(row);
  }
  $('updated').textContent='ATUALIZADO '+new Intl.DateTimeFormat('pt-BR',{timeStyle:'short'}).format(new Date());
}
async function refresh(){try{const response=await fetch('/api/jobs');if(!response.ok)throw Error('HTTP '+response.status);render(await response.json())}catch{$('updated').textContent='CONEXÃO INDISPONÍVEL'}}
refresh();setInterval(refresh,5000);
