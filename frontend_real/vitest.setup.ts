// happy-dom 不提供 window.visualViewport，Vuetify 的 overlay/v-dialog 会监听它，
// 导致测试期出现 "visualViewport is not defined" 未处理异常（继而 emitsOptions null 级联）。
// 这里补一个最小桩，让含 v-dialog 的组件测试干净通过。
if (typeof window !== 'undefined' && !(window as any).visualViewport) {
  ;(window as any).visualViewport = {
    width: 1024,
    height: 768,
    offsetLeft: 0,
    offsetTop: 0,
    pageLeft: 0,
    pageTop: 0,
    scale: 1,
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }
}
