package ir.digitalogic.pccontroller.mobile

import org.junit.Assert.*
import org.junit.Test
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.sin
import kotlin.math.sqrt

class BuzzerPcmTest {
    @Test fun boundedDelayAndMaximumDurationCannotOverflow() {
        val synth = BuzzerPcm()
        assertEquals(2400L, synth.framesForMicros(50000))
        assertEquals(12000L, synth.framesForMicros(250000))
        synth.enqueue(1000, 65535, 0)
        val pcm = ShortArray(240)
        synth.render(pcm, pcm.size, .35)
        assertTrue(pcm.any { it != 0.toShort() })
    }
    @Test fun shortNotesKeepTheirFramesAndSilence() {
        for (ms in listOf(1, 3, 10, 115)) {
            val synth = BuzzerPcm()
            synth.enqueue(880, ms, 480)
            val pcm = ShortArray(480 + ms * 48 + 480)
            synth.render(pcm, pcm.size, 0.35)
            assertTrue(pcm.take(480).all { it == 0.toShort() })
            assertTrue(pcm.slice(480 until 480 + ms * 48).any { abs(it.toInt()) > 1000 })
            assertTrue(pcm.drop(480 + ms * 48).all { it == 0.toShort() })
            assertEquals(pcm.size.toLong(), synth.cursor)
        }
    }

    @Test fun lateBurstShiftsFollowersWithoutCollapsingShortNotes() {
        val synth = BuzzerPcm()
        synth.render(ShortArray(960), 960, .35)
        synth.enqueue(1000, 1, 0)
        synth.enqueue(1500, 3, 480)
        val pcm = ShortArray(960)
        synth.render(pcm, pcm.size, .35)
        assertTrue(pcm.take(48).any { it != 0.toShort() })
        assertTrue(pcm.slice(48 until 480).all { it == 0.toShort() })
        assertTrue(pcm.slice(480 until 624).any { it != 0.toShort() })
        assertTrue(pcm.drop(624).all { it == 0.toShort() })
    }

    @Test fun chunkBoundariesAndPartialWritesDoNotChangePcm() {
        fun synth() = BuzzerPcm().apply {
            enqueue(880, 3, 0); enqueue(1200, 10, 240); enqueue(0, 4, 720)
        }
        val reference = ShortArray(1200)
        synth().render(reference, reference.size, .35)
        val buffer = BuzzerStreamBuffer(synth())
        val actual = ArrayList<Short>()
        var call = 0
        while (actual.size < reference.size) {
            buffer.write(.35) { pcm, offset, count ->
                val n = minOf(count, listOf(0, 1, 17, 79, 240)[call++ % 5], reference.size - actual.size)
                actual.addAll(pcm.slice(offset until offset + n))
                n
            }
        }
        assertArrayEquals(reference, actual.toShortArray())
    }

    @Test fun stopDiscardsUnwrittenPcmAndAllQueuedNotes() {
        val synth = BuzzerPcm()
        synth.enqueue(1000, 100, 0); synth.enqueue(2000, 100, 4800)
        val buffer = BuzzerStreamBuffer(synth)
        buffer.write(.35) { _, _, _ -> 17 }
        synth.clear()
        assertEquals(0, synth.pendingCount)
        buffer.write(.35) { pcm, offset, count ->
            assertTrue(pcm.slice(offset until offset + count).all { it == 0.toShort() })
            count
        }
    }

    @Test fun squareHasBrighterHarmonicsWithoutClippingOrDc() {
        val synth = BuzzerPcm()
        synth.enqueue(1000, 120, 0)
        val pcm = ShortArray(5760)
        synth.render(pcm, pcm.size, 1.0)
        val body = pcm.slice(480 until 5280)
        fun amplitude(hz: Int): Double {
            var re = 0.0; var im = 0.0
            body.forEachIndexed { i, x ->
                re += x * cos(2 * PI * hz * i / 48000)
                im += x * sin(2 * PI * hz * i / 48000)
            }
            return sqrt(re * re + im * im)
        }
        assertTrue(amplitude(3000) / amplitude(1000) > .25)
        assertTrue(body.maxOf { abs(it.toInt()) } < 32767)
        assertTrue(abs(body.map { it.toDouble() }.average()) < 1)
        assertEquals(0.toShort(), pcm.first())
        assertEquals(0.toShort(), pcm.last())
    }

    @Test fun boundedQueueAndPauseAreEnforced() {
        val synth = BuzzerPcm()
        repeat(32) { synth.enqueue(1000, 1, 4800 + it * 480L) }
        try { synth.enqueue(1000, 1, 24000); fail("Unbounded queue") }
        catch (_: IllegalStateException) {}
        synth.clear()
        synth.enqueue(1000, 100, 0); synth.enqueue(0, 10, 480)
        val pcm = ShortArray(1440)
        synth.render(pcm, pcm.size, .35)
        assertTrue(pcm.take(480).any { it != 0.toShort() })
        assertTrue(pcm.drop(480).all { it == 0.toShort() })
    }
}
