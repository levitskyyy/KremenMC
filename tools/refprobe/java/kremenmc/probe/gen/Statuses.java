package kremenmc.probe.gen;

import java.io.PrintStream;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.chunk.status.ChunkDependencies;
import net.minecraft.world.level.chunk.status.ChunkPyramid;
import net.minecraft.world.level.chunk.status.ChunkStatus;
import net.minecraft.world.level.chunk.status.ChunkStep;

// Prints the chunk status list and both chunk pyramids (direct and
// accumulated dependencies per ring, write radius) through public API only.
public final class Statuses {
    static PrintStream out;

    public static void main(String[] args) {
        out = System.out;
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        List<ChunkStatus> all = ChunkStatus.getStatusList();
        out.println("max_structure_distance " + ChunkStatus.MAX_STRUCTURE_DISTANCE);
        for (ChunkStatus s : all) {
            ChunkStatus p = s.getParent();
            out.println("status " + s.getIndex() + " " + s.getName() + " parent=" + (p == null ? "-" : p.getName())
                    + " type=" + s.getChunkType() + " heightmaps=" + s.heightmapsAfter()
                    + " final=" + s.heightmapsAfter().equals(ChunkStatus.FINAL_HEIGHTMAPS));
        }
        pyramid("generation", ChunkPyramid.GENERATION_PYRAMID, all);
        pyramid("loading", ChunkPyramid.LOADING_PYRAMID, all);
    }

    static void pyramid(String name, ChunkPyramid py, List<ChunkStatus> all) {
        for (ChunkStep st : py.steps()) {
            ChunkStatus t = st.targetStatus();
            StringBuilder b = new StringBuilder();
            b.append(name).append(' ').append(t.getName()).append(" write=").append(st.blockStateWriteRadius());
            b.append(" direct=").append(rings(st.directDependencies()));
            b.append(" accumulated=").append(rings(st.accumulatedDependencies()));
            b.append(" radiusOf=");
            for (ChunkStatus s : all) {
                b.append(s.getName()).append(':').append(radius(st, s)).append(',');
            }
            b.append(" directRadiusOf=");
            for (ChunkStatus s : all) {
                b.append(s.getName()).append(':').append(directRadius(st, s)).append(',');
            }
            out.println(b);
        }
    }

    static String radius(ChunkStep st, ChunkStatus s) {
        try {
            return Integer.toString(st.getAccumulatedRadiusOf(s));
        } catch (IllegalArgumentException e) {
            return "x";
        }
    }

    static String directRadius(ChunkStep st, ChunkStatus s) {
        try {
            return Integer.toString(st.directDependencies().getRadiusOf(s));
        } catch (IllegalArgumentException e) {
            return "x";
        }
    }

    static String rings(ChunkDependencies d) {
        StringBuilder b = new StringBuilder("[");
        for (int i = 0; i < d.size(); i++) {
            if (i > 0) b.append(' ');
            b.append(d.get(i).getName());
        }
        return b.append("] r=").append(d.getRadius()).toString();
    }
}
