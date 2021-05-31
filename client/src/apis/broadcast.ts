import { apis } from "@/apis/http"
import { $get } from "@/stores/localStorage"

type TypeStatus = "0" | "1" | "2"

export interface IBroadcast {
  avatar_url: string
  broadcast_id: string
  category: string
  created_at: string
  data: string
  full_name: string
  status: TypeStatus
  user_id: string
}
export const ApiGetBroadcastList = (): Promise<IBroadcast[]> =>
  apis.get(`/broadcast/${$get("group").group_id}`)

export const ApiPostBroadcast = (data: string): Promise<boolean> =>
  apis.post(`/broadcast/${$get("group").group_id}`, { data })

export const ApiGetBroadcastRecall = (broadcast_id: string): Promise<boolean> =>
  apis.get(`/broadcast/${$get("group").group_id}/${broadcast_id}`)
