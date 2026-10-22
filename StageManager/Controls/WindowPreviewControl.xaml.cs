using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Effects;
using System.Windows.Shapes;
using StageManager.Interop;
using StageManager.Models;
using StageManager.Services;
using Brushes = System.Windows.Media.Brushes;
using Point = System.Windows.Point;
using Size = System.Windows.Size;
using UserControl = System.Windows.Controls.UserControl;

namespace StageManager.Controls;

/// <summary>
/// One application in the strip: up to three of its windows stacked as
/// previews, live until the card rests and stills after that. Position,
/// presence, hover and press are springs stepped by the main window's frame loop.
/// </summary>
public partial class WindowPreviewControl : UserControl
{
    private const int MaximumLayers = 3;
    private const double StackOffset = 7;
    private const double StackShrink = 0.9;
    private const int FrontSlices = 24;
    private const int BackSlices = 10;
    // Cards rest where the eye can study them, so their edges step by a pixel at most.
    private const double SliceStep = 1;
    // Rounded corners, relative to the window's height so they shrink with the card.
    private const double CornerRadiusPerHeight = 0.05;
    private const int FrontCornerSlices = 6;
    private const int BackCornerSlices = 4;
    private const double HoverTurn = 0.6;
    private const double HoverGrowth = 0.05;
    // Like macOS, cards share one left edge so their icons line up in a column.
    private const double CardLeft = 22;
    private const double ShadowOpacity = 0.42;
    private const double ShadowBlurRadius = 22;
    // WPF's Gaussian blur, measured: its spread relative to the radius.
    private const double BlurSigmaPerRadius = 0.36;
    // How far a window's shape may drift before its still is taken again.
    private const double StillAspectTolerance = 0.01;
    private static readonly Vector ShadowOffset = new(3, 9);

    private readonly List<CardLayer> _layers = [];
    // Live previews replaced by stills, kept until their successors are on screen.
    private readonly List<SlicedThumbnail> _retired = [];
    private readonly CardLayout _layout;
    private nint _destination;
    private bool _isInFlight;
    private bool _isFrozen;
    // Counts releases, so stills that arrive after one are thrown away.
    private int _thumbnailGeneration;
    // Cards scale around the front window's left edge, which keeps that edge shared.
    private Point _scaleOrigin;

    internal WindowPreviewControl(AppGroup group, ImageSource? icon, CardLayout layout, double width)
    {
        InitializeComponent();
        _layout = layout;
        Width = width;
        Height = layout.ItemExtent;
        Group = group;
        Update(group, icon);
    }

    internal AppGroup Group { get; private set; }

    internal string Key => Group.Key;

    internal ImageSource? AppIcon { get; private set; }

    /// <summary>The card swapped its previews and needs a frame to show the new ones.</summary>
    internal event EventHandler? ThumbnailsReplaced;

    /// <summary>Vertical centre of the card's slot inside the strip, before scrolling.</summary>
    internal Spring CenterY { get; } = new(0, response: 0.42, precision: 0.05);

    /// <summary>1 while the card belongs to the strip, 0 once it has left.</summary>
    internal Spring Presence { get; } = new(0, response: 0.34);

    internal Spring Hover { get; } = new(0, response: 0.24);

    internal Spring Press { get; } = new(0, response: 0.16);

    /// <summary>The card was just created and should appear in its slot rather than slide into it.</summary>
    internal bool IsNew { get; set; } = true;

    internal bool IsRemoving { get; set; }

    /// <summary>
    /// The card's windows are currently drawn by the transition layer. The app
    /// icon leaves with them and is back once they are home, unless
    /// <see cref="RevealIcon"/> brought it in while they were still flying.
    /// </summary>
    internal bool IsInFlight
    {
        get => _isInFlight;
        set
        {
            _isInFlight = value;
            IconPresence.Snap(value ? 0 : 1);
        }
    }

    /// <summary>1 while the card shows its app icon.</summary>
    internal Spring IconPresence { get; } = new(1, response: 0.28);

    internal double ContentOpacity { get; private set; }

    /// <summary>How far the card is turned away; hovering turns it toward the viewer.</summary>
    internal double Tilt => 1 - HoverTurn * Hover.Value;

    internal double ViewportFactor { get; private set; }

    /// <summary>
    /// The shadow under a card's windows in device pixels, for flights that
    /// leave or land in a card. The offset is set in strip coordinates; the
    /// blur lives inside the card and so scales with it.
    /// </summary>
    internal static ShadowStyle GetShadowStyle(double deviceScale, double cardScale) => new(
        0,
        0,
        (float)ShadowOpacity,
        (float)(ShadowBlurRadius * BlurSigmaPerRadius * cardScale * deviceScale),
        (float)(ShadowOffset.X * deviceScale),
        (float)(ShadowOffset.Y * deviceScale));

    internal void Update(AppGroup group, ImageSource? icon)
    {
        Group = group;
        AppIcon = icon;
        var windows = group.Windows.Take(MaximumLayers).ToArray();
        if (!windows.Select(static window => window.Handle).SequenceEqual(_layers.Select(static layer => layer.Handle)) ||
            IsStillOutdated(windows))
        {
            RebuildLayers(windows);
        }
        else
        {
            for (var index = 0; index < windows.Length; index++)
            {
                _layers[index].Crop = windows[index].Crop;
                _layers[index].AspectRatio = windows[index].AspectRatio;
            }
        }

        ArrangeLayers();
    }

    internal bool Step(double seconds)
    {
        var isMoving = CenterY.Step(seconds);
        isMoving |= Presence.Step(seconds);
        // A flight aims for the card as it was when the flight left, so the card
        // holds its hover until it shows its windows again and then follows the
        // pointer from there; changing it unseen is a jump when the card appears.
        if (!IsInFlight)
        {
            isMoving |= Hover.Step(seconds);
        }

        isMoving |= Press.Step(seconds);
        isMoving |= IconPresence.Step(seconds);
        return isMoving;
    }

    /// <summary>
    /// Fades the app icon in while the card's windows are still flying into it,
    /// so it is in place as they land, as on macOS.
    /// </summary>
    internal void RevealIcon() => IconPresence.Target = 1;

    /// <summary>
    /// Whether <paramref name="point"/>, in strip coordinates, falls on the card
    /// once it rests un-hovered at <paramref name="centerY"/> with
    /// <paramref name="stripScale"/>. The whole card is hit-testable, so this is
    /// where the pointer hovers it.
    /// </summary>
    internal bool ContainsAtRest(Point point, double centerY, double stripScale)
    {
        var local = new Point(
            _scaleOrigin.X + (point.X - _scaleOrigin.X) / stripScale,
            _scaleOrigin.Y + (point.Y - centerY) / stripScale);
        return new Rect(0, 0, Width, Height).Contains(local);
    }

    /// <param name="stripScale">Shared size of all cards, shrinking as the strip fills up.</param>
    internal void ApplyVisuals(double viewportFactor, double scrollOffset, double stripScale)
    {
        ViewportFactor = viewportFactor;
        var presence = Presence.Value;
        var scale = stripScale * (0.8 + 0.2 * presence) * (1 + HoverGrowth * Hover.Value) * (1 - 0.04 * Press.Value);
        CardScale.CenterX = _scaleOrigin.X;
        CardScale.CenterY = _scaleOrigin.Y;
        CardScale.ScaleX = scale;
        CardScale.ScaleY = scale;
        CardTranslate.X = (8 * Hover.Value - 24 * (1 - presence)) * stripScale;
        CardTranslate.Y = CenterY.Value + scrollOffset - _scaleOrigin.Y;
        ContentOpacity = IsInFlight ? 0 : Math.Clamp(presence * viewportFactor, 0, 1);
        Opacity = ContentOpacity;
        IsHitTestVisible = !IsRemoving && viewportFactor > 0.5;
    }

    internal void AttachThumbnails(nint destination)
    {
        _destination = destination;
        // Back windows register first so the front window is composed on top.
        for (var index = _layers.Count - 1; index >= 0; index--)
        {
            _layers[index].Thumbnail ??= CreateThumbnail(index, _layers[index].Handle);
        }
    }

    internal void ReleaseThumbnails()
    {
        _thumbnailGeneration++;
        _isFrozen = false;
        foreach (var layer in _layers)
        {
            layer.Thumbnail?.Dispose();
            layer.Thumbnail = null;
            layer.Still?.Dispose();
            layer.Still = null;
        }

        DisposeRetired();
    }

    /// <summary>
    /// Has the card show stills of its windows instead of the live windows, so
    /// it costs nothing while it rests in the strip. The stills are taken once
    /// per set of windows; a window that cannot be captured, such as a minimized
    /// one, stays live. Flights still draw the live windows.
    /// </summary>
    internal async void Freeze()
    {
        if (_destination == 0 || _isFrozen || IsInFlight || IsRemoving || _layers.Count == 0)
        {
            return;
        }

        _isFrozen = true;
        var generation = _thumbnailGeneration;
        var layers = _layers.ToArray();
        // Large enough for the front window hovered at full size.
        var deviceScale = PresentationSource.FromVisual(this)?.CompositionTarget?.TransformToDevice.M22 ?? 1;
        var maximumHeight = (int)Math.Ceiling(_layout.PreviewHeight * (1 + HoverGrowth) * deviceScale);
        var stills = await Task.WhenAll(layers.Select(layer => WindowStill.CaptureAsync(layer.Handle, maximumHeight)));
        if (generation != _thumbnailGeneration || stills.All(static still => still is null))
        {
            foreach (var still in stills)
            {
                still?.Dispose();
            }

            return;
        }

        // The whole card is registered again, back to front, so it keeps one
        // contiguous block of the z-order with the front window on top.
        for (var index = layers.Length - 1; index >= 0; index--)
        {
            var layer = layers[index];
            layer.Still = stills[index];
            if (layer.Thumbnail is { } live)
            {
                _retired.Add(live);
            }

            layer.Thumbnail = CreateThumbnail(index, layer.Still?.Handle ?? layer.Handle);
        }

        if (!IsVisible)
        {
            // Nothing is on screen to hand over from.
            DisposeRetired();
        }

        ThumbnailsReplaced?.Invoke(this, EventArgs.Empty);
    }

    /// <param name="vanishingY">Height of the strip's centre line in <paramref name="root"/> coordinates.</param>
    internal void UpdateThumbnails(Visual root, Matrix toDevice, NativeMethods.Rect clip, double stripOpacity, double vanishingY)
    {
        var opacity = ContentOpacity * stripOpacity;
        GeneralTransform? toCard = null;
        try
        {
            toCard = TransformToAncestor(root).Inverse;
        }
        catch (InvalidOperationException)
        {
            // A layout pass can briefly detach a card while the strip refreshes.
        }

        var tilt = Tilt;
        foreach (var layer in _layers)
        {
            if (opacity <= 0.004 || toCard is null || GetLayerRootBounds(root, layer, tilt) is not { } bounds)
            {
                layer.Thumbnail?.SetVisible(false);
                continue;
            }

            // The shadow lives inside the card, so map the turned outline back
            // from window coordinates into the card's own.
            var corners = Perspective.Corners(bounds.Left, bounds.Top, bounds.Width, bounds.Height, tilt, vanishingY);
            layer.Shadow.Points = new PointCollection(
                corners.Select(corner => toCard.Transform(new Point(corner.X, corner.Y) + ShadowOffset)));

            var device = Rect.Transform(bounds, toDevice);
            layer.Thumbnail?.Show(
                device.Left,
                device.Top,
                device.Width,
                device.Height,
                layer.Crop,
                tilt,
                vanishingY * toDevice.M22 + toDevice.OffsetY,
                opacity,
                clip,
                device.Height * CornerRadiusPerHeight);
        }

        // The stills show what the live previews showed, so dropping those only
        // now, after the stills were placed, leaves no frame without either.
        DisposeRetired();
    }

    /// <summary>Where a window is drawn right now, in device pixels relative to <paramref name="root"/>.</summary>
    internal Rect? GetCurrentDeviceBounds(Visual root, Matrix toDevice, nint window) =>
        _layers.FirstOrDefault(layer => layer.Handle == window) is { } layer
            ? GetLayerDeviceBounds(root, toDevice, layer)
            : null;

    /// <summary>
    /// The window as the card shows it: turned away, it is narrower than the
    /// window facing the viewer, and it stays put along its near edge.
    /// </summary>
    private static Rect Turn(Rect bounds, double tilt) =>
        new(bounds.Left, bounds.Top, bounds.Width * Perspective.WidthScale(tilt), bounds.Height);

    /// <summary>
    /// Where a window will be drawn once the card rests at <paramref name="centerY"/>
    /// with <paramref name="stripScale"/> and its current hover, in coordinates of
    /// the strip; <see cref="Tilt"/> is how far it is turned there. Windows beyond
    /// the stack use the front window's rectangle.
    /// </summary>
    internal Rect? GetRestingLayerBounds(nint window, double centerY, double stripScale)
    {
        if ((_layers.FirstOrDefault(layer => layer.Handle == window) ?? _layers.FirstOrDefault()) is not { } layer)
        {
            return null;
        }

        // The same pose ApplyVisuals gives a fully present, unpressed card.
        var scale = stripScale * (1 + HoverGrowth * Hover.Value);
        var bounds = Turn(layer.Bounds, Tilt);
        return new Rect(
            _scaleOrigin.X + (bounds.Left - _scaleOrigin.X) * scale + 8 * Hover.Value * stripScale,
            centerY + (bounds.Top - _scaleOrigin.Y) * scale,
            bounds.Width * scale,
            bounds.Height * scale);
    }

    internal IReadOnlyList<(nint Handle, ThumbnailCrop Crop)> GetLayers() =>
        _layers.Select(static layer => (layer.Handle, layer.Crop)).ToArray();

    internal IconOverlayItem? GetBadge(Visual root, double stripOpacity)
    {
        // Unlike the windows, the icon can show while the card is in flight.
        var opacity = IconPresence.Value * Math.Clamp(Presence.Value * ViewportFactor, 0, 1) * stripOpacity;
        if (AppIcon is null || opacity <= 0.01 || _layers.Count == 0)
        {
            return null;
        }

        try
        {
            var bounds = AppIconAnchor.TransformToAncestor(root)
                .TransformBounds(new Rect(0, 0, AppIconAnchor.Width, AppIconAnchor.Height));
            return new IconOverlayItem(Key, AppIcon, bounds, opacity);
        }
        catch (InvalidOperationException)
        {
            // A layout pass can briefly detach a card while the strip refreshes.
            return null;
        }
    }

    private Rect? GetLayerDeviceBounds(Visual root, Matrix toDevice, CardLayer layer) =>
        GetLayerRootBounds(root, layer, Tilt) is { } bounds ? Rect.Transform(bounds, toDevice) : null;

    private static Rect? GetLayerRootBounds(Visual root, CardLayer layer, double tilt)
    {
        try
        {
            return Turn(
                layer.Surface.TransformToAncestor(root)
                    .TransformBounds(new Rect(0, 0, layer.Surface.Width, layer.Surface.Height)),
                tilt);
        }
        catch (InvalidOperationException)
        {
            return null;
        }
    }

    private SlicedThumbnail? CreateThumbnail(int index, nint source) => SlicedThumbnail.TryCreate(
        _destination,
        source,
        index == 0 ? FrontSlices : BackSlices,
        SliceStep,
        index == 0 ? FrontCornerSlices : BackCornerSlices);

    private void DisposeRetired()
    {
        foreach (var thumbnail in _retired)
        {
            thumbnail.Dispose();
        }

        _retired.Clear();
    }

    /// <summary>A still keeps the shape it was taken with, so a resized window is taken again.</summary>
    private bool IsStillOutdated(IReadOnlyList<WindowSnapshot> windows)
    {
        for (var index = 0; index < windows.Count; index++)
        {
            if (_layers[index].Still is not null &&
                Math.Abs(windows[index].AspectRatio - _layers[index].AspectRatio) > StillAspectTolerance)
            {
                return true;
            }
        }

        return false;
    }

    private void RebuildLayers(IReadOnlyList<WindowSnapshot> windows)
    {
        ReleaseThumbnails();
        foreach (var layer in _layers)
        {
            LayerCanvas.Children.Remove(layer.Surface);
            LayerCanvas.Children.Remove(layer.Shadow);
        }

        _layers.Clear();
        foreach (var window in windows)
        {
            var layer = new CardLayer(window.Handle, CreateSurface(), CreateShadow())
            {
                Crop = window.Crop,
                AspectRatio = window.AspectRatio
            };
            _layers.Add(layer);
            // Inserting at the front leaves the back-most layer first, so
            // shadows of nearer windows fall over the ones behind them.
            LayerCanvas.Children.Insert(0, layer.Surface);
            LayerCanvas.Children.Insert(0, layer.Shadow);
        }

        if (_destination != 0)
        {
            AttachThumbnails(_destination);
        }
    }

    private void ArrangeLayers()
    {
        if (_layers.Count == 0)
        {
            return;
        }

        var front = Fit(_layers[0].AspectRatio, 1);
        var frontTop = (Height - front.Height) / 2 + (_layers.Count - 1) * StackOffset / 2;
        for (var index = 0; index < _layers.Count; index++)
        {
            var layer = _layers[index];
            var size = index == 0 ? front : Fit(layer.AspectRatio, Math.Pow(StackShrink, index));
            layer.Bounds = new Rect(
                CardLeft,
                frontTop - StackOffset * index,
                size.Width,
                size.Height);
            layer.Surface.Width = size.Width;
            layer.Surface.Height = size.Height;
            Canvas.SetLeft(layer.Surface, layer.Bounds.Left);
            Canvas.SetTop(layer.Surface, layer.Bounds.Top);
        }

        var frontBounds = _layers[0].Bounds;
        _scaleOrigin = new Point(frontBounds.Left, frontBounds.Top + frontBounds.Height / 2);

        // Like macOS, every card has the same icon, sitting on the near bottom
        // corner and sticking out a little to the left.
        var iconSize = _layout.IconSize;
        AppIconAnchor.Width = iconSize;
        AppIconAnchor.Height = iconSize;
        Canvas.SetLeft(AppIconAnchor, frontBounds.Left - iconSize * 0.22);
        Canvas.SetTop(AppIconAnchor, frontBounds.Bottom - iconSize * 0.92);
    }

    /// <summary>
    /// Like Alt+Tab, every window gets the same height and its own width, so a
    /// wide window is not drawn as a thin strip. Only a window too wide for the
    /// strip even when hovered, and so turned toward the viewer, is made shorter.
    /// </summary>
    private Size Fit(double aspectRatio, double shrink)
    {
        var height = _layout.PreviewHeight * shrink;
        var maximumWidth = _layout.PreviewWidth * shrink /
                           (Perspective.WidthScale(1 - HoverTurn) * (1 + HoverGrowth));
        var width = height * aspectRatio;
        return width <= maximumWidth ? new Size(width, height) : new Size(maximumWidth, maximumWidth / aspectRatio);
    }

    /// <summary>An invisible layout anchor for one window; DWM draws the window over it.</summary>
    private static Border CreateSurface() => new()
    {
        IsHitTestVisible = false
    };

    /// <summary>A soft shadow under one window, following its turned outline.</summary>
    private static Polygon CreateShadow() => new()
    {
        Fill = Brushes.Black,
        Opacity = ShadowOpacity,
        IsHitTestVisible = false,
        Effect = new BlurEffect
        {
            Radius = ShadowBlurRadius,
            KernelType = KernelType.Gaussian
        }
    };

    private sealed class CardLayer(nint handle, Border surface, Polygon shadow)
    {
        internal nint Handle { get; } = handle;

        internal Border Surface { get; } = surface;

        internal Polygon Shadow { get; } = shadow;

        internal SlicedThumbnail? Thumbnail { get; set; }

        /// <summary>What <see cref="Thumbnail"/> is cut from while the card rests; null while it is live.</summary>
        internal WindowStill? Still { get; set; }

        internal ThumbnailCrop Crop { get; set; }

        internal double AspectRatio { get; set; }

        internal Rect Bounds { get; set; }
    }
}
