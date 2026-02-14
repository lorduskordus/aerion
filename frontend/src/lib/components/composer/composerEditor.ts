/**
 * TipTap editor configuration for the email composer
 */
import { Editor, Extension } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import Link from '@tiptap/extension-link'
import Underline from '@tiptap/extension-underline'
import Placeholder from '@tiptap/extension-placeholder'
import Image from '@tiptap/extension-image'
import TextStyle from '@tiptap/extension-text-style'
import Color from '@tiptap/extension-color'
import TextAlign from '@tiptap/extension-text-align'
import Table from '@tiptap/extension-table'
import TableRow from '@tiptap/extension-table-row'
import TableCell from '@tiptap/extension-table-cell'
import TableHeader from '@tiptap/extension-table-header'
import FontSize from 'tiptap-extension-font-size'

/**
 * Extended TextStyle to handle legacy <font> tags from signatures/pasted content
 */
export const ExtendedTextStyle = TextStyle.extend({
  parseHTML() {
    return [
      { tag: 'span' },
      { tag: 'font' },
    ]
  },
})

/**
 * Extended Color to handle legacy <font color="..."> tags
 */
export const ExtendedColor = Color.extend({
  addGlobalAttributes() {
    return [
      {
        types: this.options.types,
        attributes: {
          color: {
            default: null,
            parseHTML: (element: HTMLElement) => {
              const styleColor = element.style.color?.replace(/['"]+/g, '')
              if (styleColor) return styleColor
              if (element.tagName === 'FONT') {
                return element.getAttribute('color')
              }
              return null
            },
            renderHTML: (attributes: Record<string, string>) => {
              if (!attributes.color) {
                return {}
              }
              return {
                style: `color: ${attributes.color}`,
              }
            },
          },
        },
      },
    ]
  },
})

export interface ComposerEditorHandlers {
  onUpdate?: () => void
  onPasteImage?: (file: File) => void
  onDropImage?: (file: File) => void
  onShiftTab?: () => void
}

/**
 * Create a configured TipTap editor for the composer
 */
export function createComposerEditor(
  element: HTMLElement,
  handlers: ComposerEditorHandlers = {}
): Editor {
  return new Editor({
    element,
    extensions: [
      StarterKit,
      Underline,
      ExtendedTextStyle,
      ExtendedColor,
      FontSize,
      TextAlign.configure({
        types: ['paragraph'],
      }),
      Table.configure({
        resizable: false,
      }),
      TableRow,
      TableCell,
      TableHeader,
      Link.configure({
        openOnClick: false,
      }),
      Image.configure({
        inline: true,
        allowBase64: true,
      }),
      Placeholder.configure({
        placeholder: 'Write your message...',
      }),
      Extension.create({
        name: 'shiftTabHandler',
        addKeyboardShortcuts() {
          return {
            'Shift-Tab': () => {
              handlers.onShiftTab?.()
              return true
            },
            'Mod-Enter': () => true,
          }
        },
      }),
    ],
    content: '',
    editorProps: {
      attributes: {
        class: 'composer-editor focus:outline-none min-h-[200px] p-3',
      },
      // Handle paste events for images
      handlePaste: (view, event) => {
        const items = event.clipboardData?.items
        if (!items) return false

        for (const item of items) {
          if (item.type.startsWith('image/')) {
            event.preventDefault()
            const file = item.getAsFile()
            if (file && handlers.onPasteImage) {
              handlers.onPasteImage(file)
            }
            return true
          }
        }
        return false
      },
      // Handle drop events for images
      handleDrop: (view, event, slice, moved) => {
        if (moved) return false  // Let TipTap handle moves

        const files = event.dataTransfer?.files
        if (!files?.length) return false

        for (const file of files) {
          if (file.type.startsWith('image/')) {
            event.preventDefault()
            if (handlers.onDropImage) {
              handlers.onDropImage(file)
            }
            return true
          }
        }
        return false
      },
    },
    onUpdate: handlers.onUpdate,
  })
}
