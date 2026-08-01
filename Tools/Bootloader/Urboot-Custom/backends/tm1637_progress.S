/*
 * Optional PCController TM1637 progress backend for Urboot u8.0.
 *
 * SPDX-License-Identifier: GPL-3.0-only
 *
 * D13/PB5 is CLK and D11/PB3 is DIO. Both outputs are open-drain: their
 * PORTB latches remain zero and this code only changes DDRB direction bits.
 * The separately powered TM1637 retains the application's data-mode and
 * brightness commands across the external MCU reset that enters Urboot.
 *
 * ABI: r24 = backend-selected write/read event code
 *      r22 = high byte of the current flash/EEPROM address
 */

#if defined(URBOOT_PROGRESS_TM1637) && URBOOT_PROGRESS_TM1637

#if !defined(__AVR_ATmega328P__)
#error "The TM1637 Urboot backend is validated only for ATmega328P"
#endif

#define TM_DDR_IO 0x04
#define TM_CLK_BIT 5
#define TM_DIO_BIT 3

    .section .text.pccontroller_tm1637,"ax",@progbits
    .global urboot_progress
    .type urboot_progress, @function

urboot_progress:
    mov  r19, r24
    mov  r20, r22

    rcall tm_start
    ldi  r24, 0xc0
    rcall tm_byte
    mov  r24, r19
    rcall tm_byte

    /* Fill middle bars at address-space quarters: 0x20, 0x40 and 0x60. */
    ldi  r23, 3
    ldi  r25, 0x20
1:
    clr  r24
    cp   r20, r25
    brlo 2f
    ldi  r24, 0x40
2:
    rcall tm_byte
    subi r25, 0xe0
    dec  r23
    brne 1b

    rcall tm_stop
    ret
.size urboot_progress, .-urboot_progress

/* Generate a START while both released lines are high. */
tm_start:
    cbi  TM_DDR_IO, TM_CLK_BIT
    cbi  TM_DDR_IO, TM_DIO_BIT
    rcall tm_delay
    sbi  TM_DDR_IO, TM_DIO_BIT
    rcall tm_delay
    ret

/* Generate STOP and leave both pins released for the application. */
tm_stop:
    sbi  TM_DDR_IO, TM_CLK_BIT
    sbi  TM_DDR_IO, TM_DIO_BIT
    rcall tm_delay
    cbi  TM_DDR_IO, TM_CLK_BIT
    rcall tm_delay
    cbi  TM_DDR_IO, TM_DIO_BIT
    rcall tm_delay
    ret

/* Send r24 least-significant bit first, then clock the ignored ACK bit. */
tm_byte:
    ldi  r18, 8
3:
    sbi  TM_DDR_IO, TM_CLK_BIT
    sbrs r24, 0
    rjmp 4f
    cbi  TM_DDR_IO, TM_DIO_BIT
    rjmp 5f
4:
    sbi  TM_DDR_IO, TM_DIO_BIT
5:
    rcall tm_delay
    cbi  TM_DDR_IO, TM_CLK_BIT
    rcall tm_delay
    lsr  r24
    dec  r18
    brne 3b

    sbi  TM_DDR_IO, TM_CLK_BIT
    cbi  TM_DDR_IO, TM_DIO_BIT
    rcall tm_delay
    cbi  TM_DDR_IO, TM_CLK_BIT
    rcall tm_delay
    sbi  TM_DDR_IO, TM_CLK_BIT
    ret

/* 49 cycles including call/return, approximately 3.06 us at 16 MHz. */
tm_delay:
    ldi  r21, 14
6:
    dec  r21
    brne 6b
    ret

#endif
