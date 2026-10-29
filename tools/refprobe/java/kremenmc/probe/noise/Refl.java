package kremenmc.probe.noise;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.List;
import net.minecraft.world.level.levelgen.synth.GradientNoise;
import net.minecraft.world.level.levelgen.synth.Noise;
import net.minecraft.world.level.levelgen.synth.NoiseStack;
import net.minecraft.world.level.levelgen.synth.NormalNoise;
import net.minecraft.world.level.levelgen.synth.SmearedPerlinNoise;

// Reflective access to runtime state of reference noise objects.
final class Refl {
    static Object get(Object o, Class<?> c, String name) {
        try {
            Field f = c.getDeclaredField(name);
            f.setAccessible(true);
            return f.get(o);
        } catch (ReflectiveOperationException e) {
            throw new RuntimeException(e);
        }
    }

    static Object call(Object o, String name) {
        try {
            Method m = o.getClass().getDeclaredMethod(name);
            m.setAccessible(true);
            return m.invoke(o);
        } catch (ReflectiveOperationException e) {
            throw new RuntimeException(e);
        }
    }

    static byte[] perms(Noise n) {
        return (byte[]) get(n, GradientNoise.class, "perms");
    }

    static double off(Noise n, String axis) {
        return (double) get(n, GradientNoise.class, "offset" + axis);
    }

    static String gradient(Noise n) {
        StringBuilder sb = new StringBuilder(n.getClass().getSimpleName());
        sb.append(' ').append(Pts.h(off(n, "X"))).append(' ').append(Pts.h(off(n, "Y"))).append(' ').append(Pts.h(off(n, "Z"))).append(' ');
        sb.append(java.util.Arrays.hashCode(perms(n)));
        if (n instanceof SmearedPerlinNoise) sb.append(' ').append(Pts.h((double) get(n, SmearedPerlinNoise.class, "fudgeYScale")));
        return sb.toString();
    }

    static Object[] layers(NoiseStack s) {
        return (Object[]) get(s, NoiseStack.class, "layers");
    }

    // Writes "layer <depth> <freq> <amp> <noise...>" lines describing a stack.
    static void describe(StringBuilder sb, Noise n, int depth) {
        if (n == null) {
            sb.append("null\n");
            return;
        }
        if (n instanceof NoiseStack s) {
            sb.append("stack ").append(depth).append(' ').append(n.getClass().getSimpleName()).append(' ').append(Pts.h(n.range().min())).append(' ').append(Pts.h(n.range().max())).append('\n');
            for (Object l : layers(s)) {
                sb.append("layer ").append(depth).append(' ').append(Pts.h((double) call(l, "frequency"))).append(' ').append(Pts.h((float) call(l, "amplitude"))).append(' ');
                Noise c = (Noise) call(l, "noise");
                if (c instanceof NoiseStack) {
                    sb.append("sub\n");
                    describe(sb, c, depth + 1);
                } else {
                    sb.append(gradient(c)).append('\n');
                }
            }
            sb.append("end ").append(depth).append('\n');
        } else {
            sb.append("noise ").append(gradient(n)).append('\n');
        }
    }

    static String normal(NormalNoise nn) {
        Object p = get(nn, NormalNoise.class, "parameters");
        StringBuilder sb = new StringBuilder();
        sb.append("params ").append(Pts.h((double) call(p, "baseAmplitude"))).append(' ').append(call(p, "baseOctave")).append(' ')
                .append(call(p, "octaveCount")).append(' ').append(call(p, "normalize"));
        for (Object d : (List<?>) call(p, "amplitudeModifiers")) sb.append(' ').append(Pts.h((double) (Double) d));
        sb.append("\nnf ").append(Pts.h((double) get(nn, NormalNoise.class, "normalizationFactor")));
        sb.append(" range ").append(Pts.h(nn.range().min())).append(' ').append(Pts.h(nn.range().max())).append('\n');
        for (Object o : (List<?>) get(nn, NormalNoise.class, "octaves")) {
            sb.append("octave ").append(call(o, "octaveIndex")).append(' ').append(Pts.h((double) call(o, "frequency"))).append(' ')
                    .append(Pts.h((double) call(o, "amplitude"))).append(' ').append(call(o, "seed")).append('\n');
        }
        return sb.toString();
    }
}
