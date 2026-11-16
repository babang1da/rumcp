/**
 * Номер счётчика Метрики из env. Принимаем только цифры: любая другая строка
 * не попадёт в HTML страницы.
 */
export function metrikaCounterId(raw: unknown): string | null {
  if (typeof raw !== 'string') return null
  const value = raw.trim()
  return /^\d{4,12}$/.test(value) ? value : null
}

/** Сниппет Метрики: подключает tag.js и инициализирует счётчик. */
export function metrikaSnippet(id: string): string {
  return `(function(m,e,t,r,i,k,a){m[i]=m[i]||function(){(m[i].a=m[i].a||[]).push(arguments)};
m[i].l=1*new Date();
for(var j=0;j<document.scripts.length;j++){if(document.scripts[j].src===r){return;}}
k=e.createElement(t),a=e.getElementsByTagName(t)[0],k.async=1,k.src=r,a.parentNode.insertBefore(k,a)})
(window,document,'script','https://mc.yandex.ru/metrika/tag.js','ym');
ym(${id},'init',{clickmap:true,trackLinks:true,accurateTrackBounce:true});`
}

/**
 * Мета-тег подтверждения Вебмастера. Пустое или подозрительное значение
 * не рендерим — тега в HTML не будет.
 */
export function webmasterVerificationMeta(raw: unknown): {
  name: string
  content: string
} | null {
  if (typeof raw !== 'string') return null
  const value = raw.trim()
  return /^[A-Za-z0-9_-]{8,64}$/.test(value)
    ? { name: 'yandex-verification', content: value }
    : null
}
