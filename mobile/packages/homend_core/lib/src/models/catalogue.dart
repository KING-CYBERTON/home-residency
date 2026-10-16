import 'json.dart';

class ServiceArea {
  const ServiceArea({required this.id, required this.slug, required this.name, this.county});

  factory ServiceArea.fromJson(Json j) =>
      ServiceArea(id: j.str('id'), slug: j.str('slug'), name: j.str('name'), county: j.strOrNull('county'));

  final String id;
  final String slug;
  final String name;
  final String? county;
}

class Category {
  const Category({
    required this.id,
    required this.slug,
    required this.name,
    this.description,
    this.engagement = 'one_off',
    this.requiresClientIdVerification = false,
  });

  factory Category.fromJson(Json j) => Category(
    id: j.str('id'),
    slug: j.str('slug'),
    name: j.str('name'),
    description: j.strOrNull('description'),
    engagement: j.strOrNull('engagement') ?? 'one_off',
    requiresClientIdVerification: j.flag('requires_client_id_verification'),
  );

  final String id;
  final String slug;
  final String name;
  final String? description;

  /// `live_in`, `day_based` or `one_off`.
  final String engagement;
  final bool requiresClientIdVerification;
}

class RateCardEntry {
  const RateCardEntry({required this.id, required this.clientPriceMinor, this.placementFeeMinor = 0});

  factory RateCardEntry.fromJson(Json j) => RateCardEntry(
    id: j.str('id'),
    clientPriceMinor: j.intOr('client_price_minor'),
    placementFeeMinor: j.intOr('placement_fee_minor'),
  );

  final String id;
  final int clientPriceMinor;
  final int placementFeeMinor;
}

class Service {
  const Service({
    required this.id,
    required this.categoryId,
    required this.name,
    required this.pricingUnit,
    this.description,
    this.scopeItems = const [],
    this.minQuantity,
    this.maxQuantity,
    this.currentPrice,
  });

  factory Service.fromJson(Json j) => Service(
    id: j.str('id'),
    categoryId: j.str('category_id'),
    name: j.str('name'),
    pricingUnit: j.strOrNull('pricing_unit') ?? 'per_job',
    description: j.strOrNull('description'),
    scopeItems: j.strings('scope_items'),
    minQuantity: j.numOrNull('min_quantity'),
    maxQuantity: j.numOrNull('max_quantity'),
    currentPrice: j.model('current_price', RateCardEntry.fromJson),
  );

  final String id;
  final String categoryId;
  final String name;

  /// `per_job`, `per_hour`, `per_day`, `per_month` or `per_room`.
  final String pricingUnit;
  final String? description;
  final List<String> scopeItems;
  final double? minQuantity;
  final double? maxQuantity;
  final RateCardEntry? currentPrice;
}

class SubscriptionPlan {
  const SubscriptionPlan({
    required this.id,
    required this.name,
    required this.priceMinor,
    required this.billingPeriod,
    this.description,
  });

  factory SubscriptionPlan.fromJson(Json j) => SubscriptionPlan(
    id: j.str('id'),
    name: j.str('name'),
    priceMinor: j.intOr('price_minor'),
    billingPeriod: j.strOrNull('billing_period') ?? 'monthly',
    description: j.strOrNull('description'),
  );

  final String id;
  final String name;
  final int priceMinor;
  final String billingPeriod;
  final String? description;
}

class CategoryDetail {
  const CategoryDetail(this.category, this.services);

  factory CategoryDetail.fromJson(Json j) =>
      CategoryDetail(Category.fromJson(j.obj('category') ?? const {}), j.list('services', Service.fromJson));

  final Category category;
  final List<Service> services;
}
