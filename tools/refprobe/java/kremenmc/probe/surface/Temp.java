package kremenmc.probe.surface;

import java.io.BufferedOutputStream;
import java.io.PrintStream;
import java.lang.reflect.Method;
import java.util.Random;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.Registries;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.levelgen.synth.GradientNoise;
import net.minecraft.world.level.levelgen.synth.Noise;
import net.minecraft.world.level.levelgen.synth.NoiseStack;

// Temp prints biome climate settings and the height adjusted temperature
// (private Biome.getHeightAdjustedTemperature) and coldEnoughToSnow at
// random positions, plus the static temperature noises: structure (through
// reflection) and sampled values.
// Args: <points> <pointSeed>
public final class Temp {
    static String h(float v) { return Integer.toHexString(Float.floatToRawIntBits(v)); }
    static String h(double v) { return Long.toHexString(Double.doubleToRawLongBits(v)); }

    static void describe(StringBuilder sb, Noise n, int depth) throws Exception {
        if (n instanceof NoiseStack s) {
            sb.append("stack ").append(depth).append(' ').append(n.getClass().getName()).append('\n');
            for (Object l : (Object[]) Env.get(s, NoiseStack.class, "layers")) {
                double f = (double) Env.method(l.getClass(), "frequency").invoke(l);
                float a = (float) Env.method(l.getClass(), "amplitude").invoke(l);
                Noise c = (Noise) Env.method(l.getClass(), "noise").invoke(l);
                sb.append("layer ").append(depth).append(' ').append(h(f)).append(' ').append(h(a)).append(' ');
                if (c instanceof NoiseStack) {
                    sb.append("sub\n");
                    describe(sb, c, depth + 1);
                } else {
                    sb.append(c.getClass().getName()).append(' ').append(h((double) Env.get(c, GradientNoise.class, "offsetX"))).append(' ')
                        .append(java.util.Arrays.hashCode((byte[]) Env.get(c, GradientNoise.class, "perms"))).append('\n');
                }
            }
        } else {
            sb.append("noise ").append(n.getClass().getName()).append('\n');
        }
    }

    public static void main(String[] args) throws Exception {
        Env.boot();
        PrintStream o = new PrintStream(new BufferedOutputStream(Env.out, 1 << 16), false);
        try {
            int n = Integer.parseInt(args[0]);
            Random r = new Random(Long.parseLong(args[1]));
            for (String f : new String[] {"TEMPERATURE_NOISE", "FROZEN_TEMPERATURE_NOISE", "BIOME_INFO_NOISE"}) {
                Noise nz = (Noise) Env.get(null, Biome.class, f);
                StringBuilder sb = new StringBuilder("static " + f + "\n");
                describe(sb, nz, 0);
                o.print(sb);
                sb = new StringBuilder("values " + f);
                Random q = new Random(7);
                for (int i = 0; i < 64; i++) {
                    double x = (q.nextDouble() - 0.5) * (i < 32 ? 2000 : 6e7), z = (q.nextDouble() - 0.5) * 2000;
                    sb.append(' ').append(h(x)).append(',').append(h(z)).append(',').append(h(nz.get(x, z))).append(',').append(h(nz.get(x, 0, z)));
                }
                o.println(sb);
            }
            Method hat = Env.method(Biome.class, "getHeightAdjustedTemperature");
            var reg = Env.server.registryAccess().lookupOrThrow(Registries.BIOME);
            reg.listElements().sorted((a, b) -> a.key().identifier().toString().compareTo(b.key().identifier().toString())).forEach(hb -> {
                try {
                    Biome b = hb.value();
                    Object cs = Env.get(b, Biome.class, "climateSettings");
                    StringBuilder sb = new StringBuilder("biome " + hb.key().identifier() + " " + h(b.getBaseTemperature()) + " " + Env.method(cs.getClass(), "temperatureModifier").invoke(cs) + " " + b.hasPrecipitation());
                    for (int i = 0; i < n; i++) {
                        int x = r.nextInt(i % 3 == 0 ? 60_000_000 : 4000) - (i % 3 == 0 ? 30_000_000 : 2000);
                        int z = r.nextInt(4000) - 2000;
                        int y = r.nextInt(400) - 64;
                        BlockPos p = new BlockPos(x, y, z);
                        sb.append(' ').append(x).append(',').append(y).append(',').append(z).append(',').append(h((float) hat.invoke(b, p, 63))).append(',').append(b.coldEnoughToSnow(p, 63) ? 1 : 0);
                    }
                    o.println(sb);
                } catch (Exception e) {
                    throw new RuntimeException(e);
                }
            });
        } catch (Throwable t) {
            t.printStackTrace(o);
        }
        o.flush();
        Env.exit();
    }
}
