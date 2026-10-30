package net.minecraft.network.protocol.game;

import java.util.Optional;
import net.minecraft.network.chat.Component;
import net.minecraft.network.chat.numbers.NumberFormat;
import net.minecraft.network.protocol.Packet;
import net.minecraft.world.scores.Objective;
import net.minecraft.world.scores.criteria.ObjectiveCriteria;

public final class ClientboundSetObjectivePacket implements Packet<ClientGamePacketListener> {
    public static final int METHOD_ADD = 0;
    public static final int METHOD_REMOVE = 1;
    public static final int METHOD_CHANGE = 2;

    private final String objectiveName;
    private final Component displayName;
    private final ObjectiveCriteria.RenderType renderType;
    private final Optional<NumberFormat> numberFormat;
    private final int method;

    public ClientboundSetObjectivePacket(Objective objective, int method) {
        this.objectiveName = objective.getName();
        this.displayName = objective.getDisplayName();
        this.renderType = objective.getRenderType();
        this.numberFormat = Optional.ofNullable(objective.numberFormat());
        this.method = method;
    }

    public String getObjectiveName() { return objectiveName; }
    public Component getDisplayName() { return displayName; }
    public ObjectiveCriteria.RenderType getRenderType() { return renderType; }
    public Optional<NumberFormat> getNumberFormat() { return numberFormat; }
    public int getMethod() { return method; }
}
