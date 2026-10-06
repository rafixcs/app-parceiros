export type Channel = "instagram" | "tiktok" | "whatsapp" | "other";

export const channels: Record<Channel, string> = {
  instagram: "Instagram",
  tiktok: "TikTok",
  whatsapp: "WhatsApp",
  other: "Outro",
};

type CopyableItem = {
  title: string;
  description: string;
  product: { name: string };
  affiliate_link: string | null;
  link_origin: "auto" | "manual";
  links: { channel: Channel; url: string }[];
};

/** The item's link for a channel: the manual one counts for every channel; otherwise the channel's or the main one. */
export function channelLink(item: CopyableItem, channel: Channel): string | null {
  if (item.link_origin === "manual") return item.affiliate_link;
  return item.links.find((l) => l.channel === channel)?.url ?? item.affiliate_link;
}

/** Title, description and link, separated by a blank line, ready to paste on a social network. */
export function textToCopy(item: CopyableItem, channel: Channel): string {
  const title = item.title.trim() || item.product.name;
  return [title, item.description.trim(), channelLink(item, channel) ?? ""].filter(Boolean).join("\n\n");
}

/** Copies to the clipboard; returns false when the browser does not allow it. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
