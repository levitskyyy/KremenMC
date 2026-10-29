package kremenmc.probe.noise;

import java.util.Random;

// Coordinate generator shared by noise probes: small, block-aligned,
// chunk/region borders, far lands and huge values.
final class Pts {
    final Random r;

    Pts(long seed) {
        r = new Random(seed);
    }

    double c() {
        return switch (r.nextInt(10)) {
            case 0 -> (r.nextDouble() - 0.5) * 4;
            case 1 -> (r.nextInt(2001) - 1000);
            case 2 -> (r.nextInt(4001) - 2000) * 0.25;
            case 3 -> ((r.nextInt(401) - 200) * 16 + r.nextInt(3) - 1) * (r.nextBoolean() ? 1 : 0.25);
            case 4 -> ((r.nextInt(81) - 40) * 512 + r.nextInt(3) - 1);
            case 5 -> (r.nextDouble() - 0.5) * 60_000_000;
            case 6 -> (r.nextInt(3) - 1) * 30_000_000 + r.nextInt(33) - 16 + (r.nextBoolean() ? 0 : r.nextDouble());
            case 7 -> (r.nextDouble() - 0.5) * Math.pow(2, r.nextInt(40));
            case 8 -> (r.nextBoolean() ? 1 : -1) * Math.pow(2, 20 + r.nextInt(14)) + r.nextInt(5) - 2 + r.nextDouble();
            default -> (r.nextDouble() - 0.5) * 2000;
        };
    }

    double y() {
        return r.nextInt(3) == 0 ? c() : r.nextInt(448) - 64 + (r.nextInt(4) == 0 ? r.nextDouble() : 0);
    }

    static String h(double v) {
        return Long.toUnsignedString(Double.doubleToRawLongBits(v), 16);
    }

    static String h(float v) {
        return Integer.toUnsignedString(Float.floatToRawIntBits(v), 16);
    }
}
