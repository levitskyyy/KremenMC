package kremenmc.probe.feat_nether;

import java.lang.reflect.Proxy;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.WorldGenLevel;
import net.minecraft.world.level.levelgen.feature.EndSpikeFeature;

// Spikes prints EndSpikeFeature.getSpikesForLevel for each seed argument,
// through a WorldGenLevel proxy that only answers getSeed:
//   <seed> <i> <centerX> <centerZ> <radius> <height> <guarded>
public final class Spikes {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        for (String a : args) {
            long seed = Long.parseLong(a);
            WorldGenLevel wl = (WorldGenLevel) Proxy.newProxyInstance(Spikes.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, (p, m, x) -> {
                if (m.getName().equals("getSeed")) return seed;
                throw new UnsupportedOperationException(m.getName());
            });
            int i = 0;
            for (EndSpikeFeature.EndSpike s : EndSpikeFeature.getSpikesForLevel(wl)) {
                System.out.println(seed + " " + i++ + " " + s.getCenterX() + " " + s.getCenterZ() + " " + s.getRadius() + " " + s.getHeight() + " " + s.isGuarded());
            }
        }
        System.out.flush();
        Runtime.getRuntime().halt(0);
    }
}
