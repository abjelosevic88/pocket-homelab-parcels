export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'
export type AppSlug = 'mail' | 'money' | 'wishlist' | 'inventory' | 'calendar' | 'tasks' | 'contacts' | 'documents'

type ConnFields = { [K in AppSlug as `${K}_mode`]: ConnMode } & { [K in AppSlug as `${K}_via`]: ConnVia } & { [K in AppSlug as `${K}_detail` | `${K}_url` | `${K}_token` | `${K}_public_url` | `${K}_open_url`]: string } & { [K in AppSlug as `${K}_configured`]: boolean }

export type Settings = ConnFields & {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number; currency: string
  track_cainiao: boolean; track17_key: string; track17_configured: boolean; auto_archive_days: number; stale_days: number
  notify_moves: boolean; notify_out: boolean; notify_pickup: boolean; notify_delivered: boolean; notify_problems: boolean; notify_returns: boolean; notify_stale: boolean
  notify_hour: number; notify_url: string; notify_format: string; notify_configured: boolean
  mail_scan: boolean; mail_auto_add: boolean; mail_days: number
  money_account_id: number; money_category_id: number
  wishlist_bought: boolean; inventory_auto: boolean; inventory_location: string
  calendar_sync: boolean; calendar_returns: boolean; calendar_id: number
  tasks_pickup: boolean; tasks_returns: boolean
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string
}

export type Status = 'pending' | 'info_received' | 'in_transit' | 'out_for_delivery' | 'pickup' | 'delivered' | 'exception' | 'returned' | 'expired'
export type ReturnState = '' | 'keeping' | 'returning' | 'returned'

export interface ParcelEvent { id: number; parcel_id: number; at: string; status: Status; description: string; location: string; source: string; created_at: string }
export interface Attachment { id: number; parcel_id: number; kind: 'document' | 'url'; ref: number | null; url: string; title: string; created_at: string }

export interface Parcel {
  id: number; title: string; tracking_number: string; carrier: string; direction: 'in' | 'out'; shop: string; order_number: string; ordered_on: string
  price: number | null; extra_cost: number | null; currency: string
  status: Status; status_text: string; status_at: string; location: string
  expected_on: string; expected_until: string; expected_manual: boolean; delivered_at: string
  return_by: string; return_state: ReturnState; notes: string; archived: boolean
  provider: string; checked_at: string; next_check_at: string; check_error: string
  source: string; external_id: string; mail_message_id: number | null
  wish_id: number | null; wish_title: string; money_tx_id: number | null; money_tx_label: string; extra_tx_id: number | null; inventory_item_id: number | null
  contact_id: number | null; contact_name: string; task_id: number | null; return_task_id: number | null
  created_at: string; updated_at: string
  days?: number; event_count?: number; events?: ParcelEvent[]; attachments?: Attachment[]
  // view
  carrier_name: string; status_label: string; tracking_url: string; url: string; stale: boolean; arrives_in: number | null
  mail_url?: string; wish_url?: string; money_url?: string; extra_url?: string; item_url?: string; task_url?: string; return_task_url?: string; contact_url?: string
  can_check: boolean; check_with?: string
}

export interface Overview {
  date: string; needs_you: Parcel[]; on_the_way: Parcel[]; problems: Parcel[]; recent: Parcel[]; returns: Parcel[]; finds: number
  counts: { active: number; needs_you: number; on_the_way: number; problems: number; returns: number; delivered_month: number }
  tracking: { cainiao: boolean; seventeen: boolean }
}

export interface Find {
  id: number; mail_message_id: number; subject: string; from_name: string; from_addr: string; received_at: string
  tracking_number: string; carrier: string; shop: string; order_number: string; title: string; status_hint: string
  state: 'new' | 'added' | 'updated' | 'dismissed'; parcel_id: number | null; created_at: string; carrier_name: string; mail_url: string
}

export interface Carrier { key: string; name: string; country?: string; cainiao: boolean }
export interface Detect { number: string; valid: boolean; carrier: string; carrier_name: string; tracking_url: string; check_with: string; existing_id?: number; existing_title?: string }

export interface Tally { name: string; parcels: number; delivered: number; avg_days: number }
export interface Stats {
  months: { month: string; parcels: number; delivered: number; avg_days: number }[]
  shops: Tally[]; carriers: Tally[]; spent: Record<string, number>; extra: Record<string, number>; avg_days: number; delivered: number
}

export interface Picked<T> { connected: boolean; detail?: string; items: T[] }
export interface MoneyAccount { id: number; name: string; kind: string; currency: string; everyday: boolean }
export interface MoneyCategory { id: number; name: string; full_name: string }
export interface MoneyTx { id: number; kind: string; date: string; account_id: number; amount: number; amount_home: number; currency: string; description: string; payee: string }
export interface CalendarItem { id: number; name: string; color: string; writable: boolean; is_default: boolean }
export interface ContactItem { id: number; fn: string; org: string; title: string; emails: string[] | null; phones: string[] | null }
export interface DocumentItem { id: number; title: string; original_name: string; created: string; added_at: string; correspondent_name: string; document_type_name: string }
export interface Wish { id: number; list_id: number; title: string; url: string; store: string; price: number; currency: string; status: string; bought_at: string }
