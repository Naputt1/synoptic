export function goto(href: string) {
  const path = href.startsWith('/') ? href : '/' + href
  if (window.location.hash === '#' + path) return
  window.location.hash = '#' + path
}

export function goBack() {
  window.history.back()
}
