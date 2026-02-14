/**
 * Signature management utilities for the email composer
 */
// @ts-ignore - Wails generated imports
import { account } from '../../../../wailsjs/go/models'

// Re-export the Identity type for use in other files
export type Identity = account.Identity

export type ComposeMode = 'new' | 'reply' | 'reply-all' | 'forward'

/**
 * Signature marker - used to identify where signature starts for removal/swapping
 * Using three zero-width spaces as an invisible marker that TipTap preserves in text nodes
 */
export const SIGNATURE_MARKER = '\u200B\u200B\u200B'
export const SIGNATURE_MARKER_REGEX = /\u200B\u200B\u200B[\s\S]*$/

/**
 * Build signature HTML from identity settings
 */
export function buildSignatureHtml(identity: Identity): string {
  if (!identity.signatureEnabled) return ''
  if (!identity.signatureHtml) return ''

  let html = ''

  // Add separator line if enabled (with marker at the start)
  if (identity.signatureSeparator) {
    html = `<p>${SIGNATURE_MARKER}-- </p>`
  } else {
    // Inject marker into the first element of the signature
    // This ensures TipTap preserves it as text content
    html = identity.signatureHtml.replace(/^(<p[^>]*>)/, `$1${SIGNATURE_MARKER}`)
    // If signature doesn't start with <p>, wrap the marker
    if (!html.includes(SIGNATURE_MARKER)) {
      html = `<p>${SIGNATURE_MARKER}</p>` + identity.signatureHtml
    }
  }

  // If we added separator, append the rest of the signature
  if (identity.signatureSeparator) {
    html += identity.signatureHtml
  }

  return html
}

/**
 * Check if signature should be appended for the given compose mode
 */
export function shouldAppendSignature(identity: Identity, mode: ComposeMode): boolean {
  if (!identity.signatureEnabled) return false

  switch (mode) {
    case 'new':
      return identity.signatureForNew
    case 'reply':
    case 'reply-all':
      return identity.signatureForReply
    case 'forward':
      return identity.signatureForForward
    default:
      return false
  }
}

/**
 * Insert signature into editor content based on compose mode and placement settings
 */
export function insertSignatureIntoContent(
  content: string,
  signatureHtml: string,
  mode: ComposeMode,
  placement: string = 'above'
): string {
  if (placement === 'above') {
    // Look for a citation line ("On DATE, SENDER wrote:") to insert signature above it.
    // Search for "wrote:" followed by optional <br> and closing </p> tag.
    // This works regardless of compose mode or whether TipTap preserves blockquotes.
    const wroteMatch = content.match(/wrote:\s*(<br[^>]*>)?\s*<\/p>/i)
    if (wroteMatch && wroteMatch.index !== undefined) {
      const before = content.substring(0, wroteMatch.index)
      const pStart = before.lastIndexOf('<p')
      if (pStart > -1) {
        const quotedContent = content.substring(pStart)
        // typing area + blank line below content + signature + 2 blank lines before citation
        return '<p></p><p></p>' + signatureHtml + '<p></p><p></p>' + quotedContent
      }
    }

    // Fallback: try blockquote
    const blockquoteIndex = content.indexOf('<blockquote')
    if (blockquoteIndex > -1) {
      const blockquote = content.substring(blockquoteIndex)
      // typing area + blank line below content + signature + 2 blank lines before citation
      return '<p></p><p></p>' + signatureHtml + '<p></p><p></p>' + blockquote
    }
  }

  // New message or no quoted content found
  const isEmpty = content === '<p></p>' || content === '' || content === '<p><br></p>'
  if (isEmpty) {
    // typing area + blank line below content + signature
    return '<p></p><p></p>' + signatureHtml
  }
  return content + '<p></p>' + signatureHtml
}

/**
 * Remove signature from content using the marker
 */
export function removeSignatureFromContent(content: string): string {
  // Remove everything from the signature marker to the end
  let result = content.replace(SIGNATURE_MARKER_REGEX, '')
  // Clean up trailing <br> tags that were before the signature
  result = result.replace(/(<br\s*\/?>)+\s*$/, '')
  return result
}

/**
 * Check if content already contains a signature marker
 */
export function hasSignatureMarker(content: string): boolean {
  return content.includes(SIGNATURE_MARKER)
}
