// Shared terminal presentation primitives for every project-owned Node tool.
// Chalk owns styling and cli-table3 owns width, alignment, padding, and drawing.

import { Chalk } from 'chalk'
import Table from 'cli-table3'

const TABLE_GLYPHS = Object.freeze({
	'top': '─', 'top-mid': '┬', 'top-left': '╭', 'top-right': '╮',
	'bottom': '─', 'bottom-mid': '┴', 'bottom-left': '╰', 'bottom-right': '╯',
	'left': '│', 'left-mid': '├', 'mid': '─', 'mid-mid': '┼',
	'right': '│', 'right-mid': '┤', 'middle': '│'
})

export function createChalk(options = {}, isTTY = process.stdout.isTTY) {
	const enabled = !options.noColor && (isTTY || options.forceColor)
	return new Chalk({ level: enabled ? 3 : 0 })
}

function coloredGlyphs(chalk, color = 'cyan') {
	const draw = typeof chalk[color] === 'function' ? chalk[color] : value => value
	return Object.fromEntries(
		Object.entries(TABLE_GLYPHS).map(([name, glyph]) => [name, draw(glyph)])
	)
}

export function renderUnicodeTable(columns, rows, options = {}) {
	if (!Array.isArray(columns) || columns.length === 0) return ''
	const chalk = options.chalk || createChalk(options, options.isTTY)
	const table = new Table({
		chars: coloredGlyphs(chalk, options.borderColor || 'cyan'),
		head: columns.map(column => ({
			content: chalk.bold.magentaBright(String(column.label)),
			hAlign: 'center'
		})),
		colAligns: columns.map(column => column.align || 'left'),
		style: { head: [], border: [], compact: true, 'padding-left': 1, 'padding-right': 1 },
		wordWrap: false
	})
	for (const row of rows) {
		table.push(columns.map((_, index) => String(row[index] ?? '')))
	}
	return table.toString()
}

export function renderUnicodeBanner(lines, options = {}) {
	const chalk = options.chalk || createChalk(options, options.isTTY)
	const width = Math.max(12, Number(options.width) || 50)
	const table = new Table({
		chars: coloredGlyphs(chalk, options.borderColor || 'magentaBright'),
		colWidths: [width],
		colAligns: ['center'],
		style: { head: [], border: [], compact: true, 'padding-left': 1, 'padding-right': 1 }
	})
	for (const line of lines) table.push([line])
	return table.toString()
}
