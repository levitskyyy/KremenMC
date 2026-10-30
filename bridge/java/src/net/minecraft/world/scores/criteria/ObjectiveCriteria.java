package net.minecraft.world.scores.criteria;

public class ObjectiveCriteria {
    public static final ObjectiveCriteria DUMMY = new ObjectiveCriteria("dummy", false, RenderType.INTEGER);
    public static final ObjectiveCriteria TRIGGER = new ObjectiveCriteria("trigger", false, RenderType.INTEGER);
    public static final ObjectiveCriteria HEALTH = new ObjectiveCriteria("health", true, RenderType.HEARTS);

    private final String name;
    private final boolean readOnly;
    private final RenderType renderType;

    protected ObjectiveCriteria(String name, boolean readOnly, RenderType renderType) {
        this.name = name;
        this.readOnly = readOnly;
        this.renderType = renderType;
    }

    public String getName() { return name; }
    public boolean isReadOnly() { return readOnly; }
    public RenderType getDefaultRenderType() { return renderType; }

    public enum RenderType {
        INTEGER("integer"),
        HEARTS("hearts");

        private final String id;

        RenderType(String id) { this.id = id; }

        public String getId() { return id; }
        public String getSerializedName() { return id; }

        public static RenderType byId(String id) {
            for (RenderType t : values()) if (t.id.equals(id)) return t;
            return INTEGER;
        }
    }
}
