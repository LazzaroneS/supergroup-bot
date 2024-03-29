import vConsole from "vconsole"
import { requestConfig } from "./apis/http"
import { $get, $set } from "@/stores/localStorage"

export const request = requestConfig
if (
  process.env.NODE_ENV === "development" &&
  navigator.userAgent.includes("Mixin")
)
  new vConsole()

let envLang = process.env.LANG
let lang = navigator.language

if (envLang === 'en-US') {
  if (lang.includes('zh')) {
    $set("umi_locale", 'zh-CN')
  } else if (lang.includes('ja')) {
    $set("umi_locale", "ja")
  } else {
    $set("umi_locale", "en-US")
  }
} else {
  $set("umi_locale", envLang)
}
  
export function modifyClientRenderOpts(memo: any) {
  return {
    ...memo,
    rootElement: memo.rootElement,
  }
}
